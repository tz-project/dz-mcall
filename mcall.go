package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/pat"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
)

const (
	DefaultWorkerNum       = 10
	DefaultTimeout         = 10
	DefaultHTTPHost        = "localhost"
	DefaultHTTPPort        = "3000"
	DefaultFormat          = "json"
	DefaultLogLevel        = "DEBUG"
	DefaultLogFile         = "/var/log/mcall/mcall.log"
	DefaultChannelSize     = 100
	DefaultTimeoutDuration = DefaultTimeout * time.Second

	LogFormat = "%{color}%{time:15:04:05.000000} %{shortfunc} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}"

	// Error codes
	ErrorCodeSuccess = "0"
	ErrorCodeFailure = "-1"

	// Request types
	RequestTypeCmd  = "cmd"
	RequestTypeGet  = "get"
	RequestTypePost = "post"

	// HTTP methods
	HTTPMethodGet  = "GET"
	HTTPMethodPost = "POST"

	// Content types
	ContentTypeJSON = "application/json"
)

// Config holds all configuration settings
type Config struct {
	Worker struct {
		Number int `mapstructure:"number"`
	} `mapstructure:"worker"`

	WebServer struct {
		Enable bool   `mapstructure:"enable"`
		Host   string `mapstructure:"host"`
		Port   string `mapstructure:"port"`
	} `mapstructure:"webserver"`

	Response struct {
		Format   string `mapstructure:"format"`
		Encoding struct {
			Type string `mapstructure:"type"`
		} `mapstructure:"encoding"`
		ES struct {
			Host      string `mapstructure:"host"`
			ID        string `mapstructure:"id"`
			Password  string `mapstructure:"password"`
			IndexName string `mapstructure:"index_name"`
		} `mapstructure:"es"`
	} `mapstructure:"response"`

	Request struct {
		Subject string `mapstructure:"subject"`
		Timeout int    `mapstructure:"timeout"`
		Input   string `mapstructure:"input"`
		Type    string `mapstructure:"type"`
		Name    string `mapstructure:"name"`
	} `mapstructure:"request"`

	Log struct {
		Level string `mapstructure:"level"`
		File  string `mapstructure:"file"`
	} `mapstructure:"log"`
}

// App represents the main application
type App struct {
	config    *Config
	logger    *logging.Logger
	workerNum int
	timeout   int
	subject   string
	format    string
	base64    string
	esConfig  ESConfig
}

// ESConfig holds Elasticsearch configuration
type ESConfig struct {
	Host      string
	ID        string
	Password  string
	IndexName string
}

// FetchedResult represents the result of a fetch operation
type FetchedResult struct {
	Input   string `json:"input"`
	Name    string `json:"name"`
	Error   string `json:"errorCode"`
	Content string `json:"result"`
	TS      string `json:"ts"`
}

// FetchedInput tracks processed inputs to avoid duplicates
type FetchedInput struct {
	m map[string]error
	sync.RWMutex
}

// NewFetchedInput creates a new FetchedInput instance
func NewFetchedInput() *FetchedInput {
	return &FetchedInput{
		m: make(map[string]error),
	}
}

// IsProcessed checks if an input has already been processed
func (fi *FetchedInput) IsProcessed(input string) bool {
	fi.RLock()
	defer fi.RUnlock()
	_, exists := fi.m[input]
	return exists
}

// MarkProcessed marks an input as processed
func (fi *FetchedInput) MarkProcessed(input string, err error) {
	fi.Lock()
	defer fi.Unlock()
	fi.m[input] = err
}

// Commander interface for executing commands
type Commander interface {
	Execute() error
}

// CallFetch represents a fetch operation
type CallFetch struct {
	fetchedInput *FetchedInput
	pipeline     *Pipeline
	input        string
	sType        string
	name         string
	result       chan FetchedResult
}

// NewCallFetch creates a new CallFetch instance
func NewCallFetch(fetchedInput *FetchedInput, pipeline *Pipeline, input, sType, name string) *CallFetch {
	return &CallFetch{
		fetchedInput: fetchedInput,
		pipeline:     pipeline,
		input:        input,
		sType:        sType,
		name:         name,
		result:       make(chan FetchedResult, 1),
	}
}

// Execute implements the Commander interface
func (cf *CallFetch) Execute() error {
	if cf.fetchedInput.IsProcessed(cf.input) {
		return nil
	}

	var doc string
	var err error

	if cf.input != "" {
		switch cf.sType {
		case RequestTypeCmd:
			doc, err = fetchCmd(cf.input)
		case RequestTypeGet:
			doc, err = fetchHTTP(cf.input, HTTPMethodGet, nil)
		case RequestTypePost:
			// For POST requests, we might need to extract data from the URL
			// This is a simplified implementation - you might want to enhance it
			doc, err = fetchHTTP(cf.input, HTTPMethodPost, nil)
		default:
			// Default to GET for unknown types
			doc, err = fetchHTTP(cf.input, HTTPMethodGet, nil)
		}
	}

	cf.fetchedInput.MarkProcessed(cf.input, err)

	content := cf.parseContent(doc)
	var errCode string
	if err != nil {
		errCode = ErrorCodeFailure
	} else {
		errCode = ErrorCodeSuccess
	}

	now := time.Now().UTC()
	result := FetchedResult{
		Input:   cf.input,
		Name:    cf.name,
		Error:   errCode,
		Content: content,
		TS:      now.Format("2006-01-02T15:04:05.000"),
	}

	cf.result <- result
	return nil
}

// parseContent processes the fetched content and triggers next requests
func (cf *CallFetch) parseContent(doc string) string {
	// This is a simplified version - you might want to implement
	// the logic to trigger next requests based on your requirements
	return doc
}

// Pipeline manages worker goroutines
type Pipeline struct {
	request chan Commander
	done    chan struct{}
	wg      *sync.WaitGroup
}

// NewPipeline creates a new Pipeline instance
func NewPipeline() *Pipeline {
	return &Pipeline{
		request: make(chan Commander, DefaultChannelSize),
		done:    make(chan struct{}),
		wg:      new(sync.WaitGroup),
	}
}

// Worker processes commands from the request channel
func (p *Pipeline) Worker() {
	for {
		select {
		case r, ok := <-p.request:
			if !ok {
				return
			}
			if err := r.Execute(); err != nil {
				// Log error for debugging and monitoring
				// Note: In a production environment, you might want to use a proper logger
				fmt.Printf("Worker failed to execute command: %v\n", err)
			}
		case <-p.done:
			return
		}
	}
}

// Run starts the worker goroutines
func (p *Pipeline) Run(workerNum int) {
	p.wg.Add(workerNum)
	for i := 0; i < workerNum; i++ {
		go func() {
			defer p.wg.Done()
			p.Worker()
		}()
	}
}

// Stop gracefully stops the pipeline
func (p *Pipeline) Stop() {
	close(p.done)
	p.wg.Wait()
}

// ResultDoc represents command execution result
type ResultDoc struct {
	Raw   string `json:"raw"`
	Error string `json:"error"`
}

// fetchHTML fetches HTML content from a URL
func fetchHTML(input string) (string, error) {
	return fetchHTTP(input, HTTPMethodGet, nil)
}

// fetchHTTP fetches content from a URL with specified method and data
func fetchHTTP(input string, method string, data map[string]interface{}) (string, error) {
	if input == "" {
		return "", nil
	}

	var req *http.Request
	var err error

	if method == HTTPMethodPost && data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return "", fmt.Errorf("failed to marshal POST data: %w", err)
		}

		req, err = http.NewRequest(HTTPMethodPost, input, bytes.NewBuffer(jsonData))
		if err != nil {
			return "", fmt.Errorf("failed to create POST request: %w", err)
		}
		req.Header.Set("Content-Type", ContentTypeJSON)
	} else {
		req, err = http.NewRequest(method, input, nil)
		if err != nil {
			return "", fmt.Errorf("failed to create %s request: %w", method, err)
		}
	}

	client := &http.Client{
		Timeout: DefaultTimeoutDuration,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute %s request: %w", method, err)
	}
	defer resp.Body.Close()

	doc, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return string(doc), nil
}

// fetchCmd executes a shell command
func fetchCmd(input string) (string, error) {
	if input == "" {
		return "", nil
	}

	doc, err := exeCmd(input)
	if err != nil {
		return doc, fmt.Errorf("command execution failed: %w", err)
	}

	return doc, nil
}

// exeCmd executes a shell command with timeout
func exeCmd(str string) (string, error) {
	parts := strings.Fields(str)
	if len(parts) == 0 {
		return "", errors.New("empty command")
	}

	cmdName := parts[0]
	args := parts[1:]

	// Clean up arguments
	for i := range args {
		if args[i] == "'Content-Type_application/json'" {
			args[i] = "'Content-Type: application/json'"
		} else {
			args[i] = strings.Replace(args[i], "`", " ", -1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeoutDuration)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdName, args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", errors.New("command execution timed out")
		}
		return string(output), fmt.Errorf("command failed: %w", err)
	}

	return string(output), nil
}

// execCmd executes commands and returns results
func (app *App) execCmd(inputs []string, types []string, names []string) []map[string]string {
	start := time.Now()

	pipeline := NewPipeline()
	pipeline.Run(app.workerNum)
	defer pipeline.Stop()

	// Set default values
	if len(types) == 0 {
		types = []string{RequestTypeCmd}
	}
	if len(names) == 0 {
		names = []string{app.subject}
	}

	fetchedInput := NewFetchedInput()
	results := make([]map[string]string, 0, len(inputs))

	// Create and submit fetch requests
	for i, input := range inputs {
		sType := types[0]
		if i < len(types) {
			sType = types[i]
		}

		name := names[0]
		if i < len(names) {
			name = names[i]
		}

		call := NewCallFetch(fetchedInput, pipeline, input, sType, name)
		pipeline.request <- call

		// Wait for result
		result := <-call.result

		// Format result
		formattedResult := app.formatResult(result)
		results = append(results, formattedResult)
	}

	elapsed := time.Since(start)
	app.logger.Debugf("Execution completed in %v", elapsed)

	return results
}

// formatResult formats a single result based on app configuration
func (app *App) formatResult(result FetchedResult) map[string]string {
	formatted := make(map[string]string)

	if app.format == "json" {
		if app.subject != "" {
			formatted["subject"] = app.subject
		}
		formatted["input"] = result.Input
		formatted["name"] = result.Name
		formatted["errorCode"] = result.Error

		// Handle content encoding
		var content string
		if app.base64 == "std" {
			content = base64.StdEncoding.EncodeToString([]byte(result.Content))
		} else if app.base64 == "url" {
			content = base64.URLEncoding.EncodeToString([]byte(result.Content))
		} else {
			content = result.Content
		}
		formatted["result"] = content
		formatted["ts"] = result.TS
	} else {
		formatted["result"] = result.Content
	}

	return formatted
}

// makeResponse creates the response for HTTP requests
func (app *App) makeResponse(inputs []string, types []string, names []string) []byte {
	result := app.execCmd(inputs, types, names)

	if app.format == "json" {
		b, err := json.Marshal(result)
		if err != nil {
			app.logger.Errorf("Failed to marshal response: %v", err)
			return []byte("{}")
		}

		// Handle Elasticsearch if configured
		if app.esConfig.Host != "" {
			app.sendToElasticsearch(b)
		}

		fmt.Println(string(b))
		return b
	} else {
		// Format for non-JSON output
		var output strings.Builder
		for _, r := range result {
			output.WriteString("\n")
			output.WriteString(r["result"])
			output.WriteString("\n=============================================================\n")
		}
		fmt.Print(output.String())
		return []byte("")
	}
}

// sendToElasticsearch sends results to Elasticsearch
func (app *App) sendToElasticsearch(data []byte) {
	// Implementation for sending to Elasticsearch
	// This is a placeholder - implement based on your requirements
	app.logger.Debug("Sending to Elasticsearch (not implemented)")
}

// PrettyString formats JSON string with indentation
func PrettyString(str string) (string, error) {
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, []byte(str), "", "    "); err != nil {
		return "", err
	}
	return prettyJSON.String(), nil
}

// HTTP handlers
func (app *App) getHandle(w http.ResponseWriter, r *http.Request) {
	sType := r.URL.Query().Get(":type")
	name := r.URL.Query().Get(":name")
	paramStr := r.URL.Query().Get(":params")

	app.logger.Debugf("GET request - type: %s, name: %s, params: %s", sType, name, paramStr)

	inputs, types, names := app.parseInputParams(paramStr)
	response := app.makeResponse(inputs, types, names)

	w.Header().Set("Content-Type", "application/json")
	w.Write(response)
}

func (app *App) postHandle(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		app.logger.Errorf("Failed to parse form: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	sType := r.FormValue("type")
	if sType == "" {
		app.logger.Warning("Missing type parameter")
		http.Error(w, "Missing type parameter", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	paramStr := r.FormValue("params")
	if paramStr == "" {
		app.logger.Warning("Missing params parameter")
		http.Error(w, "Missing params parameter", http.StatusBadRequest)
		return
	}

	app.logger.Debugf("POST request - type: %s, name: %s, params: %s", sType, name, paramStr)

	inputs, types, names := app.parseInputParams(paramStr)
	response := app.makeResponse(inputs, types, names)

	w.Header().Set("Content-Type", "application/json")
	w.Write(response)
}

// parseConfigInput parses input configuration from config file
func (app *App) parseConfigInput(inputStr string) ([]string, []string, []string) {
	type Inputs struct {
		Inputs []map[string]interface{} `json:"inputs"`
	}

	var data Inputs
	if err := json.Unmarshal([]byte(inputStr), &data); err != nil {
		app.logger.Errorf("Failed to unmarshal config input: %v", err)
		return nil, nil, nil
	}

	var inputs, types, names []string

	for _, item := range data.Inputs {
		if input, exists := item["input"]; exists {
			if str, ok := input.(string); ok {
				inputs = append(inputs, str)
			}
		}
		if inputType, exists := item["type"]; exists {
			if str, ok := inputType.(string); ok {
				types = append(types, str)
			}
		}
		if name, exists := item["name"]; exists {
			if str, ok := name.(string); ok {
				names = append(names, str)
			}
		}
	}

	return inputs, types, names
}

// parseInputParams parses input parameters from JSON or base64 encoded string
func (app *App) parseInputParams(paramStr string) ([]string, []string, []string) {
	type Inputs struct {
		Inputs []map[string]interface{} `json:"inputs"`
	}

	var data Inputs

	// Try base64 decode first
	if decoded, err := base64.StdEncoding.DecodeString(paramStr); err == nil {
		if err := json.Unmarshal(decoded, &data); err != nil {
			app.logger.Errorf("Failed to unmarshal base64 decoded params: %v", err)
		}
	} else {
		// Try direct JSON unmarshal
		if err := json.Unmarshal([]byte(paramStr), &data); err != nil {
			app.logger.Errorf("Failed to unmarshal params: %v", err)
		}
	}

	var inputs, types, names []string

	for _, item := range data.Inputs {
		if input, exists := item["input"]; exists {
			if str, ok := input.(string); ok {
				inputs = append(inputs, str)
			}
		}
		if inputType, exists := item["type"]; exists {
			if str, ok := inputType.(string); ok {
				types = append(types, str)
			}
		}
		if name, exists := item["name"]; exists {
			if str, ok := name.(string); ok {
				names = append(names, str)
			}
		}
	}

	return inputs, types, names
}

// webserver starts the HTTP server
func (app *App) webserver() {
	killch := make(chan os.Signal, 1)
	signal.Notify(killch, os.Interrupt, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)

	go func() {
		<-killch
		app.logger.Infof("Shutting down server at %s", time.Now().String())
		os.Exit(0)
	}()

	r := pat.New()
	r.Get("/healthcheck", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "OK")
	})
	r.Get("/mcall/{type}/{params}", app.getHandle)
	r.Post("/mcall", app.postHandle)

	http.Handle("/", r)

	addr := fmt.Sprintf("%s:%s", app.config.WebServer.Host, app.config.WebServer.Port)
	app.logger.Infof("Starting server on %s", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		app.logger.Fatalf("Server failed to start: %v", err)
	}
}

// NewApp creates a new App instance
func NewApp(config *Config) *App {
	app := &App{
		config:    config,
		workerNum: config.Worker.Number,
		timeout:   config.Request.Timeout,
		subject:   config.Request.Subject,
		format:    config.Response.Format,
		base64:    config.Response.Encoding.Type,
		esConfig: ESConfig{
			Host:      config.Response.ES.Host,
			ID:        config.Response.ES.ID,
			Password:  config.Response.ES.Password,
			IndexName: config.Response.ES.IndexName,
		},
	}

	// Set defaults
	if app.workerNum == 0 {
		app.workerNum = DefaultWorkerNum
	}
	if app.timeout == 0 {
		app.timeout = DefaultTimeout
	}
	if app.format == "" {
		app.format = DefaultFormat
	}

	return app
}

// setupLogging configures the logging system
func setupLogging(config *Config) (*logging.Logger, error) {
	logFile := config.Log.File
	if logFile == "" {
		logFile = DefaultLogFile
	}

	// Try to create log directory, but fallback to current directory if permission denied
	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		// Fallback to current directory
		logFile = "./mcall.log"
	}

	logFileHandle, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	logBackend := logging.NewLogBackend(logFileHandle, "", 0)
	logFormatter := logging.NewBackendFormatter(logBackend, logging.MustStringFormatter(LogFormat))

	logLevel := config.Log.Level
	if logLevel == "" {
		logLevel = DefaultLogLevel
	}

	level, err := logging.LogLevel(logLevel)
	if err != nil {
		level = logging.DEBUG
	}

	logging.SetBackend(logFormatter)
	logging.SetLevel(level, "")

	return logging.MustGetLogger("mcall"), nil
}

// loadConfig loads configuration from file or sets defaults
func loadConfig(configFile string) (*Config, error) {
	config := &Config{}

	if configFile != "" {
		viper.SetConfigFile(configFile)
		viper.SetConfigType("yaml")

		if err := viper.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		if err := viper.Unmarshal(config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	// Set defaults for missing values
	if config.Worker.Number == 0 {
		config.Worker.Number = DefaultWorkerNum
	}
	if config.WebServer.Host == "" {
		config.WebServer.Host = DefaultHTTPHost
	}
	if config.WebServer.Port == "" {
		config.WebServer.Port = DefaultHTTPPort
	}
	if config.Response.Format == "" {
		config.Response.Format = DefaultFormat
	}
	if config.Request.Timeout == 0 {
		config.Request.Timeout = DefaultTimeout
	}
	if config.Log.Level == "" {
		config.Log.Level = DefaultLogLevel
	}
	if config.Log.File == "" {
		config.Log.File = DefaultLogFile
	}

	return config, nil
}

// Args represents command line arguments
type Args map[string]interface{}

// mainExec is the main execution logic
func mainExec(args Args) error {
	config, err := loadConfig(args["c"].(string))
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Override config with command line arguments
	if webserver := args["w"].(bool); webserver {
		config.WebServer.Enable = true
	}
	if port := args["p"].(string); port != "" {
		config.WebServer.Port = port
	}

	// Setup logging
	logger, err := setupLogging(config)
	if err != nil {
		return fmt.Errorf("failed to setup logging: %w", err)
	}

	// Create app instance
	app := NewApp(config)
	app.logger = logger

	// Override config with command line arguments
	if workerNum := args["worker"].(int); workerNum > 0 {
		app.workerNum = workerNum
	}
	if format := args["f"].(string); format != "" {
		app.format = format
	}
	if base64 := args["e"].(string); base64 != "" {
		app.base64 = base64
	}

	// Set runtime configuration
	numCPUs := runtime.NumCPU()
	runtime.GOMAXPROCS(numCPUs)

	app.logger.Debugf("Worker number: %d", app.workerNum)
	app.logger.Debugf("Web server enabled: %v", config.WebServer.Enable)
	app.logger.Debugf("HTTP host: %s", config.WebServer.Host)
	app.logger.Debugf("HTTP port: %s", config.WebServer.Port)

	// Run application
	if config.WebServer.Enable {
		app.webserver()
	} else {
		// Handle command line input or config file input
		var inputs []string
		var types []string
		var names []string

		if input := args["i"].(string); input != "" {
			// Command line input takes precedence
			inputs = strings.Split(input, ",")
			for i, inp := range inputs {
				inputs[i] = strings.TrimSpace(inp)
			}

			// Determine request types
			requestType := args["t"].(string)
			types = make([]string, len(inputs))
			for i := range inputs {
				if strings.HasPrefix(inputs[i], "http://") || strings.HasPrefix(inputs[i], "https://") {
					types[i] = requestType
				} else {
					types[i] = RequestTypeCmd
				}
			}

			// Set names
			names = make([]string, len(inputs))
			if name := args["n"].(string); name != "" {
				for i := range names {
					names[i] = name
				}
			}
		} else if config.Request.Input != "" {
			// Parse config file input
			inputs, types, names = app.parseConfigInput(config.Request.Input)
		}

		if len(inputs) > 0 {
			app.makeResponse(inputs, types, names)
		}
	}

	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: mcall <command> [options]")
		fmt.Println("Commands:")
		fmt.Println("  -i      - Execute command or HTTP request")
		fmt.Printf("  -t      - Request type (get, post, cmd) default: %s\n", RequestTypeCmd)
		fmt.Println("  -w      - Run webserver")
		fmt.Println("  -c      - Configuration file path")
		fmt.Println("  -help   - Show help")
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  mcall -i=\"ls /etc/hosts\"")
		fmt.Printf("  mcall -t=%s -i=\"http://localhost:%s/healthcheck\"\n", RequestTypeGet, DefaultHTTPPort)
		fmt.Printf("  mcall -t=%s -i=\"http://localhost:8000/uptime_list?company_id=1\"\n", RequestTypePost)
		fmt.Println("  mcall -w=true")
		fmt.Println("  mcall -c=/etc/mcall/mcall.yaml")
		return
	}

	// Parse command line flags
	var (
		help    = flag.Bool("help", false, "Show these options")
		vt      = flag.String("t", RequestTypeCmd, "Request type (get, post, cmd)")
		vi      = flag.String("i", "", "Input (command or URL, multiple separated by comma)")
		vc      = flag.String("c", "", "Configuration file path")
		vw      = flag.Bool("w", false, "Run webserver")
		vp      = flag.String("p", DefaultHTTPPort, "Webserver port")
		vf      = flag.String("f", DefaultFormat, "Return format (json, plain)")
		ve      = flag.String("e", "", "Return result with encoding (std, url)")
		vn      = flag.String("n", "", "Request name")
		vworker = flag.Int("worker", DefaultWorkerNum, "Number of workers")
		vlf     = flag.String("lf", DefaultLogFile, "Logfile destination")
		vll     = flag.String("l", DefaultLogLevel, "Log level (debug, info, error)")
	)
	flag.Parse()

	args := Args{
		"help":     *help,
		"t":        *vt,
		"i":        *vi,
		"c":        *vc,
		"w":        *vw,
		"p":        *vp,
		"f":        *vf,
		"e":        *ve,
		"n":        *vn,
		"worker":   *vworker,
		"logfile":  *vlf,
		"loglevel": *vll,
	}

	if args["help"] == true {
		flag.PrintDefaults()
		return
	}

	if err := mainExec(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
