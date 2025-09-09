package main

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/gorilla/pat"
	"github.com/robfig/cron"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
	"io"
	"io/ioutil"
	"log"
	_ "math/big"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	CONFIGFILE    string
	WORKERNUM     = 10
	G_TIMEOUT     = 10
	HOSTNAME      string
	SUBJECT       string
	INPUTS        []string
	STYPES        []string
	STYPE         string
	NAMES         []string
	NAME          string
	USERNAME      string
	EXPECTS       []string
	EXPECT        string
	EXECS         []string
	EXEC          string
	EXECRECEIVERS []string
	EXECRECEIVER  string
	TIMEOUT       int
	TIMEOUTS      []int
	FORMAT        string
	WEBENABLED    = false
	WEB_AUTH      string
	BASE64        string
	HTTPHOST      = "localhost"
	HTTPPORT      = "3000"
	ESCONF        ES
	CRONJOB       string
	RULES         ES_RULE
	SMTPS         SMTP_TYPE
	SLACKS        SLACK_TYPE
	RECEIVERS     RECEIVER_TYPE
)

type ES struct {
	host      string
	id        string
	password  string
	indexName string
}

type ES_RULE struct {
	ES_RULE []map[string]interface{} `json:"rules"`
}

type SMTP struct {
	host     string
	user     string
	password string
}

type SMTP_TYPE struct {
	SMTP_TYPE []map[string]interface{} `json:"smtps"`
}

type SLACK struct {
	name    string
	api_url string
}

type SLACK_TYPE struct {
	SLACK_TYPE []map[string]interface{} `json:"slacks"`
}

type RECEIVER_TYPE struct {
	RECEIVER_TYPE []map[string]interface{} `json:"receivers"`
}

type FetchedResult struct {
	input   string
	name    string
	err     string
	content string
	ts      string
}

type FetchedInput struct {
	m map[string]error
	sync.Mutex
}

type Commander interface {
	command()
}

type CallFetch struct {
	fetchedInput *FetchedInput
	p            *Pipeline
	input        string
	sType        string
	name         string
	expect       string
	exec         string
	timeout      int
	execreceiver string
	result       chan FetchedResult
}

func fetchHtml(g *CallFetch) (string, error) {
	if g.input == "" {
		return "", nil
	}
	log.Println("= input: ", g.input)
	tlsConfig := tls.Config{}
	tlsConfig.InsecureSkipVerify = true
	timeout := g.timeout
	if g.timeout == 0 {
		timeout = G_TIMEOUT
	}
	//c := http.Client{
	//	Transport: &http.Transport{
	//		TLSClientConfig: &tlsConfig,
	//	},
	//  Timeout: time.Duration(timeout) * time.Second
	//	}
	c := http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   time.Duration(timeout) * time.Second,
				KeepAlive: time.Duration(timeout) * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   time.Duration(timeout) * time.Second,
			ResponseHeaderTimeout: time.Duration(timeout) * time.Second,
			ExpectContinueTimeout: time.Duration(timeout) * time.Second,
			DisableKeepAlives:     true,
		},
	}
	res, err := c.Get(g.input)
	if err != nil {
		log.Println(err)
		return err.Error(), err
	}
	defer res.Body.Close()
	doc, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Println(err)
		return "", err
	} else {
		log.Println(string(doc))
		return string(doc), checkRslt(g, strconv.Itoa(res.StatusCode))
	}
}

func fetchCmd(g *CallFetch) (string, error) {
	if g.input == "" {
		return "", nil
	}
	log.Println("= input: ", g.input)
	timeout := g.timeout
	if g.timeout == 0 {
		timeout = G_TIMEOUT
	}
	log.Println("========= timeout: ", timeout)
	doc, err := exeCmd(g.input, timeout)
	if err != nil {
		log.Println("= fetchCmd1:", err)
		return doc, err
	} else {
		log.Println("========= fetchCmd2:", doc)
		return doc, checkRslt(g, doc)
	}
	return doc, nil
}

func checkRslt(g *CallFetch, res string) error {
	log.Println("=============================checkRslt0")
	if g.expect != "" {
		log.Println("=============================checkRslt1")
		expectArry := strings.Split(g.expect, "|")
		bOk := false
		err1 := errors.New("")
		for i := range expectArry {
			if strings.Contains(expectArry[i], "$count < ") || strings.Contains(expectArry[i], " > $count") ||
				strings.Contains(expectArry[i], "$count > ") || strings.Contains(expectArry[i], " < $count") {
				raw := expectArry[i]
				nres, err := strconv.Atoi(strings.TrimSpace(res))
				if err != nil {
					log.Println("Error: %s", err)
				}
				ntarget := 0
				if strings.Contains(raw, "$count < ") || strings.Contains(raw, " > $count") {
					if strings.Contains(raw, "$count < ") {
						ntarget, err = strconv.Atoi(strings.TrimSpace(raw[len("$count <"):len(raw)]))
					} else {
						ntarget, err = strconv.Atoi(strings.TrimSpace(raw[0:strings.Index(raw, "> $count")]))
					}
					if err != nil {
						log.Println("Error: %s", err)
					}
					if nres > ntarget {
						err1 = errors.New(fmt.Sprintf("expect: $count < %s but res: %s", ntarget, nres))
					}
				} else {
					if strings.Contains(raw, "< $count") {
						ntarget, err = strconv.Atoi(strings.TrimSpace(raw[0:strings.Index(raw, "< $count")]))
					} else {
						ntarget, err = strconv.Atoi(strings.TrimSpace(raw[len("$count >"):len(raw)]))
					}
					if err != nil {
						log.Println("Error: %s", err)
					}
					if nres < ntarget {
						err1 = errors.New(fmt.Sprintf("expect: $count > %s but res: %s", ntarget, nres))
					}
				}
				if err1.Error() != "" {
					return errorHandle(g, err1)
				}
			} else {
				if bOk == false {
					if strings.Contains(res, expectArry[i]) {
						bOk = true
						err1 = errors.New("")
					} else {
						err1 = errors.New(fmt.Sprintf("expect: %s but res: %s", g.expect, res))
					}
				}
			}
		}
		if err1.Error() != "" {
			log.Println(err1)
			return err1
		}
	}
	return nil
}

func errorHandle(g *CallFetch, err1 error) error {
	doc := err1.Error()
	receiverArry := strings.Split(g.execreceiver, ",")
	for i := range receiverArry {
		sendMsg(receiverArry[i], g.name, g.name, doc)
	}
	if g.exec == "" {
		return err1
	}
	log.Println("need to " + g.exec)
	doc, err := exeCmd(g.exec, g.timeout)
	if err != nil {
		log.Println("bash error: %s", err)
	} else {
		log.Println(doc)
	}
	return err1
}

type ResultDoc struct {
	Raw   string `json:"raw"`
	Error string `json:"error"`
}

func exeCmd(str string, timeout int) (string, error) {
	res := ResultDoc{}
	resultchan := make(chan string)
	errchan := make(chan error, 10)
	cmdName := ""
	var args []string
	if strings.HasPrefix(str, "bash -c ") {
		cmdName = "bash"
		args = append(args, "-c")
		args = append(args, str[len("bash -c ")+1:len(str)-1])
	} else {
		parts := strings.Fields(str)
		cmdName = parts[0]
		args = parts[1:len(parts)]
		// replace "`" to " "
		for n := range args {
			if args[n] == "'Content-Type_application/json'" {
				args[n] = "'Content-Type: application/json'"
			} else {
				args[n] = strings.Replace(args[n], "`", " ", -1)
			}
		}
	}
	log.Println("= cmdName: ", cmdName)
	log.Println("= args: ", args)
	cmd := exec.Command(cmdName, args...)
	stdout, err := cmd.StdoutPipe()
	if werr, ok := err.(*exec.ExitError); ok {
		if s := werr.Error(); s != "0" {
			errchan <- err
		}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Println("Error: %s", err)
	}
	go func() {
		stdo, err := ioutil.ReadAll(stdout)
		if err != nil {
			errchan <- err
		}
		resultchan <- string(stdo[:])
		stde, f := ioutil.ReadAll(stderr)
		if f != nil {
			log.Println(f)
			res.Error = string(stde)
		}
	}()
	err = cmd.Start()
	if err != nil {
		errchan <- err
		res.Error = fmt.Sprintf("Runner: %s", err.Error())
		if res.Error != "" {
			res.Raw = res.Error
		}
		log.Println("= res.Error2: ", res.Error)
		return res.Raw, errors.New(res.Error)
	}

loop:
	for {
		var aTimeout time.Duration = 5
		if timeout > 0 {
			aTimeout = time.Duration(timeout) * time.Second
		} else {
			aTimeout = time.Duration(1000000) * time.Hour
		}
		select {
		case <-time.After(aTimeout):
			cmd.Process.Kill()
			res.Error = "Runner: timedout"
			res.Raw = res.Error
			log.Println("------------------------------- timeout: " + string(timeout))
			log.Println("= res.Error1: ", res.Error)
			if timeout < 0 {
				os.Exit(1)
			}
			break loop
		case err := <-errchan:
			res.Error = fmt.Sprintf("Runner: %s", err.Error())
			if res.Error != "" {
				res.Raw = res.Error
			}
			log.Println("= res.Error2: ", res.Error)
			if timeout < 0 {
				os.Exit(1)
			}
			break loop
		case cmdresult := <-resultchan:
			if cmdresult != "" {
				res.Raw = cmdresult
				log.Println("= cmdresult: ", cmdresult)
				break loop
			}
		}
	}
	cmd.Wait()
	if res.Error == "" {
		res.Error = "Runner: OK"
		return res.Raw, nil
	}
	return res.Raw, errors.New(res.Error)
}

func (g *CallFetch) request(input string, sType string, name string, expect string, exec string, timeout int, execreceiver string) {
	g.p.request <- &CallFetch{
		fetchedInput: g.fetchedInput,
		p:            g.p,
		input:        input,
		sType:        sType,
		name:         name,
		expect:       expect,
		exec:         exec,
		timeout:      timeout,
		execreceiver: execreceiver,
		result:       g.result,
	}
}

func (g *CallFetch) parseContent(doc string) <-chan string {
	content := make(chan string)
	go func() {
		content <- doc
		chk := false
		input := ""
		sType := ""
		name := ""
		expect := ""
		exec := ""
		execreceiver := ""
		timeout := 0
		g.fetchedInput.Lock()
		for n := range INPUTS {
			if _, ok := g.fetchedInput.m[INPUTS[n]]; !ok {
				chk = true
				input = INPUTS[n]
				sType = STYPES[n]
				name = NAMES[n]
				if len(EXPECTS) > n {
					expect = EXPECTS[n]
				}
				if len(EXECS) > n {
					exec = EXECS[n]
				}
				if len(TIMEOUTS) > n {
					timeout = TIMEOUTS[n]
				}
				if len(EXECRECEIVERS) > n {
					execreceiver = EXECRECEIVERS[n]
				}
				g.request(input, sType, name, expect, exec, timeout, execreceiver)
				break
			}
		}
		if chk == false {
		}
		g.fetchedInput.Unlock()
	}()
	return content
}

func (g *CallFetch) command() {
	g.fetchedInput.Lock()
	if _, ok := g.fetchedInput.m[g.input]; ok {
		g.fetchedInput.Unlock()
		return
	}
	g.fetchedInput.Unlock()
	var doc string
	var err error
	if g.input != "" {
		if g.sType == "cmd" {
			doc, err = fetchCmd(g)
			//if err != nil {
			//	go func(u string) {
			//		g.request(u, sType)
			//	}(g.input)
			//	return
			//}
		} else {
			doc, err = fetchHtml(g)
			//if err != nil {
			//	go func(u string) {
			//		g.request(u, sType)
			//	}(g.input)
			//	return
			//}
		}
	}
	g.fetchedInput.Lock()
	g.fetchedInput.m[g.input] = err
	g.fetchedInput.Unlock()
	content := <-g.parseContent(doc)
	var errCode string
	if err != nil {
		errCode = "-1"
	} else {
		errCode = "0"
	}
	now := time.Now().UTC()
	g.result <- FetchedResult{g.input, g.name, errCode, content, now.Format("2006-01-02T15:04:05.000")}
}

type Pipeline struct {
	request chan Commander
	done    chan struct{}
	wg      *sync.WaitGroup
}

func NewPipeline() *Pipeline {
	return &Pipeline{
		request: make(chan Commander),
		done:    make(chan struct{}),
		wg:      new(sync.WaitGroup),
	}
}

func (p *Pipeline) Worker() {
	for r := range p.request {
		select {
		case <-p.done:
			return
		default:
			r.command()
		}
	}
}

func (p *Pipeline) Run() {
	p.wg.Add(WORKERNUM)
	for i := 0; i < WORKERNUM; i++ {
		go func() {
			p.Worker()
			p.wg.Done()
		}()
	}

	go func() {
		p.wg.Wait()
	}()
}

func execCmd() []map[string]string {
	start := time.Now()
	numCPUs := runtime.NumCPU()
	runtime.GOMAXPROCS(numCPUs)

	p := NewPipeline()
	p.Run()

	var sType string
	if len(STYPES) < 1 {
		sType = "cmd"
	} else {
		sType = STYPES[0]
	}
	var name string
	if len(NAMES) < 1 {
		name = NAME
	} else {
		name = NAMES[0]
	}
	var expect string
	if len(EXPECTS) < 1 {
		expect = NAME
	} else {
		expect = EXPECTS[0]
	}
	var exec string
	if len(EXECS) < 1 {
		exec = EXEC
	} else {
		exec = EXECS[0]
	}
	var timeout int
	if len(TIMEOUTS) < 1 {
		timeout = TIMEOUT
	} else {
		timeout = TIMEOUTS[0]
	}
	var execreceiver string
	if len(EXECRECEIVERS) < 1 {
		execreceiver = EXECRECEIVER
	} else {
		execreceiver = EXECRECEIVERS[0]
	}
	call := &CallFetch{
		fetchedInput: &FetchedInput{m: make(map[string]error)},
		p:            p,
		input:        INPUTS[0],
		sType:        sType,
		name:         name,
		expect:       expect,
		exec:         exec,
		timeout:      timeout,
		execreceiver: execreceiver,
		result:       make(chan FetchedResult),
	}
	p.request <- call

	var result = make([]map[string]string, 0)
	count := 0
	log.Println("============ len(INPUTS): ", len(INPUTS))
	for a := range call.result {
		count++
		var arry = make(map[string]string)
		if FORMAT == "json" {
			var rslt string
			str, _ := json.Marshal(a.content)
			if BASE64 == "std" {
				rslt = base64.StdEncoding.EncodeToString(str)
			} else if BASE64 == "url" {
				rslt = base64.URLEncoding.EncodeToString(str)
			} else {
				rslt = string(str)
			}
			if SUBJECT != "" {
				arry["subject"] = SUBJECT
			}
			arry["hostname"] = HOSTNAME
			arry["subject"] = SUBJECT
			arry["input"] = a.input
			arry["name"] = a.name
			arry["errorCode"] = a.err
			arry["result"] = rslt
			arry["ts"] = a.ts
		} else {
			arry["result"] = a.content
		}
		result = append(result, arry)
		log.Println("============ count: ", count)
		if count >= len(INPUTS) {
			close(p.done)
			break
		}
	}
	elapsed := time.Since(start)
	log.Println("elapsed: ", elapsed)
	return result
}

func execKubectl(input string, profile string) []byte {
	//pi := strings.Index(input, "--profile")
	//profile := input[pi+len("--profile")+1 : len(input)]
	//input = input[:pi-1]
	input = strings.Replace(input, "kubectl", "kubectl --kubeconfig ~/.kube/"+profile, -1)
	esCmd := "bash export AWS_PROFILE=" + profile + ";" + input
	log.Println("esCmd: %s", esCmd)
	var timestamp = strconv.FormatInt(time.Now().Unix(), 10)
	shFile := fmt.Sprintf("/tmp/kubectl_%s.sh", timestamp)
	esFile, err := os.OpenFile(shFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		log.Println("esFile error: %s %s", esFile, err)
	}
	esFile.WriteString(esCmd)
	esFile.Close()
	doc, err := exeCmd("bash "+shFile, 30)
	if err != nil {
		log.Println("bash error: %s", err)
	}
	log.Println(doc)
	os.Remove(shFile)
	now := time.Now().UTC()
	var result = make([]map[string]string, 0)
	var arry = make(map[string]string)
	arry["subject"] = "kubectl"
	arry["hostname"] = "-"
	arry["input"] = esCmd
	arry["name"] = USERNAME
	arry["errorCode"] = "0"
	arry["result"] = doc
	arry["ts"] = now.Format("2006-01-02T15:04:05.000")
	result = append(result, arry)
	b, err := json.Marshal(result)
	if err != nil {
		log.Println("error: %s", err)
	}
	outStr := string(b)
	log.Println(outStr)
	outStr = "{ \"index\":{} }\n" + outStr[1:len(outStr)-1] + "\n"
	outStr = strings.Replace(outStr, "\"\\\"", "\"", -1)
	outStr = strings.Replace(outStr, "\\\"", "", -1)
	outStr = strings.Replace(outStr, "},{", "}\n{ \"index\":{} }\n{", -1)
	log.Println("execKubectl: %s", outStr)
	indexName := "kubectl-" + now.Format("2006")
	execES(indexName, outStr)
	return []byte(doc)
}

func execES(indexName string, input string) {
	if ESCONF.host == "" {
		return
	}
	log.Println("============ ES logging ")
	var timestamp = strconv.FormatInt(time.Now().Unix(), 10)
	jsonFile := fmt.Sprintf("/tmp/es_%s.json", timestamp)
	jsFile, err := os.OpenFile(jsonFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		log.Println("jsFile error: %s %s", jsFile, err)
	}
	log.Println("execES %s", input)
	jsFile.WriteString(input)
	jsFile.Close()

	adminPassword := ESCONF.id + ":" + ESCONF.password
	esUrl := ESCONF.host
	esCmd := "curl -k -XPOST -u '" + adminPassword + "' " + esUrl + "/" + indexName + "/_bulk -H 'Content-Type: application/json' --data-binary @" + jsonFile
	shFile := fmt.Sprintf("/tmp/essh_%s.sh", timestamp)
	esFile, err := os.OpenFile(shFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		log.Println("esFile error: %s %s", esFile, err)
	}
	log.Println("esCmd %s", esCmd)
	esFile.WriteString(esCmd)
	esFile.Close()
	doc, err := exeCmd("bash "+shFile, 30)
	if err != nil {
		log.Println("bash error: %s", err)
	} else {
		log.Println(doc)
	}
	os.Remove(jsonFile)
	os.Remove(shFile)
	//cmd := exec.Command("/bin/sleep", "30")
	//err2 := cmd.Run()
	//if err2 != nil {
	//	log.Println("sleep: %s", err2)
	//}
}

// http://localhost:3000/mcall/cmd/{"inputs":[{"input":"ls -al"},{"input":"ls"}]}
func getHandle(w http.ResponseWriter, r *http.Request) {
	STYPE = r.URL.Query().Get(":type")
	NAME = r.URL.Query().Get(":name")
	paramStr := r.URL.Query().Get(":params")
	log.Println(STYPE, paramStr)
	getInput(paramStr)
	b := makeResponse(30)
	w.Write(b)
}

// http://localhost:3000/mcall?type=post&params={"inputs":[{"input":"ls -al"},{"input":"pwd"}]}
func postHandle(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		log.Println("ParseForm %s", err)
	}
	log.Println("\n what we got was %+v\n", r.Form)
	if STYPE = r.FormValue("type"); STYPE == "" {
		log.Println(fmt.Sprintf("bad STYPE received %+v", r.Form["type"]))
		return
	}
	NAME = r.FormValue("name")
	var paramStr = ""
	if paramStr = r.FormValue("params"); paramStr == "" {
		log.Println(fmt.Sprintf("bad params received %+v", r.Form["params"]))
		return
	}
	log.Println(STYPE, paramStr)
	getInput(paramStr)
	b := makeResponse(30)
	io.WriteString(w, string(b))
}

func makeResponse(timeout int) []byte {
	input := INPUTS[0]
	if strings.HasPrefix(input, "kubectl") {
		return execKubectl(input, STYPES[0])
	}
	result := execCmd()
	if FORMAT == "json" {
		b, err := json.Marshal(result)
		if err != nil {
			log.Println("error: %s", err)
		}
		outStr := string(b)
		fmt.Println(outStr)
		if ESCONF.host != "" {
			log.Println("============ ES logging ")
			//outStr = "{\"values\": " + outStr + "}"
			//outStr = strings.Replace(outStr, "\"\\\"", "\"", -1)
			//outStr = strings.Replace(outStr, "\\\"", "", -1)
			//outStr = strings.Replace(outStr, "\"", "\\\"", -1)
			//esCmd := "curl -XPOST -u '" + adminPassword + "' " + esUrl + "/" + indexName + "/doc -H 'Content-Type: application/json' -d \"" + outStr + "\""

			// make a json
			outStr = "{ \"index\":{} }\n" + outStr[1:len(outStr)-1] + "\n"
			outStr = strings.Replace(outStr, "\"\\\"", "\"", -1)
			outStr = strings.Replace(outStr, "\\\"", "", -1)
			outStr = strings.Replace(outStr, "},{", "}\n{ \"index\":{} }\n{", -1)
			var timestamp = strconv.FormatInt(time.Now().Unix(), 10)
			jsonFile := fmt.Sprintf("/tmp/test_%s.json", timestamp)
			jsFile, err := os.OpenFile(jsonFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
			if err != nil {
				log.Println("jsFile error: %s %s", jsFile, err)
			}
			log.Println("outStr %s", outStr)
			jsFile.WriteString(outStr)
			jsFile.Close()

			// make a shell
			adminPassword := ESCONF.id + ":" + ESCONF.password
			esUrl := ESCONF.host
			now := time.Now().UTC()
			indexName := ESCONF.indexName + "-" + now.Format("2006.01.02")
			esCmd := "curl -k -XPOST -u '" + adminPassword + "' " + esUrl + "/" + indexName + "/_bulk -H 'Content-Type: application/json' --data-binary @" + jsonFile
			shFile := fmt.Sprintf("/tmp/test_%s.sh", timestamp)
			esFile, err := os.OpenFile(shFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
			if err != nil {
				log.Println("esFile error: %s %s", esFile, err)
			}
			log.Println("esCmd %s", esCmd)
			esFile.WriteString(esCmd)
			esFile.Close()
			doc, err := exeCmd("bash "+shFile, timeout)
			if err != nil {
				log.Println("bash error: %s", err)
			} else {
				log.Println(doc)
				os.Remove(jsonFile)
				os.Remove(shFile)
			}
			//cmd := exec.Command("/bin/sleep", "30")
			//err2 := cmd.Run()
			//if err2 != nil {
			//	log.Println("sleep: %s", err2)
			//}
		}
		return b
	} else {
		var rslt []string
		for i := range result {
			rslt = append(rslt, "\n")
			rslt = append(rslt, result[i]["result"])
			rslt = append(rslt, "=============================================================")
			rslt = append(rslt, "\n")
		}
		fmt.Println(rslt)
		return []byte("")
	}
}

func PrettyString(str string) (string, error) {
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, []byte(str), "", "    "); err != nil {
		return "", err
	}
	return prettyJSON.String(), nil
}

func webserver() {
	killch := make(chan os.Signal, 1)
	signal.Notify(killch, os.Interrupt)
	signal.Notify(killch, syscall.SIGTERM)
	signal.Notify(killch, syscall.SIGINT)
	signal.Notify(killch, syscall.SIGQUIT)
	go func() {
		<-killch
		log.Println("Interrupt %s", time.Now().String())
	}()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		r := pat.New()
		r.Get("/healthcheck", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "OK")
		})
		r.Get("/mcall/{type}/{params}", getHandle)
		r.Post("/mcall", postHandle)
		http.Handle("/", r)
		log.Println("Listening: ", HTTPHOST, HTTPPORT)
		var err error
		if WEB_AUTH == "basic-auth" {
			AuthFile("./auth.env")
			auth := BasicAuthHandler(r)
			err = http.ListenAndServe(HTTPHOST+":"+HTTPPORT, auth)
		} else {
			err = http.ListenAndServe(HTTPHOST+":"+HTTPPORT, nil)
		}
		if err != nil {
			log.Println("ListenAndServe: ", err)
		}
		wg.Done()
	}()

	wg.Wait()
}

var Authorised func(string, string) bool
var users map[string]string

func AuthFile(file string) {
	users = make(map[string]string)
	b, err := ioutil.ReadFile(file)
	if err != nil {
		log.Fatalf("[HTTP] Error reading auth-file: %s", err)
		os.Exit(1)
	}
	buf := bytes.NewBuffer(b)
	for {
		l, err := buf.ReadString('\n')
		l = strings.TrimSpace(l)
		if len(l) > 0 {
			p := strings.SplitN(l, ":", 2)
			if len(p) < 2 {
				log.Fatalf("[HTTP] Error reading auth-file, invalid line: %s", l)
				os.Exit(1)
			}
			users[p[0]] = strings.TrimSpace(p[1])
		}
		switch {
		case err == io.EOF:
			break
		case err != nil:
			log.Fatalf("[HTTP] Error reading auth-file: %s", err)
			os.Exit(1)
			break
		}
		if err == io.EOF {
			break
		} else if err != nil {
		}
	}
	log.Printf("[HTTP] Loaded %d users from %s", len(users), file)
	Authorised = func(u, pw string) bool {
		hpw, ok := users[u]
		if !ok {
			return false
		}
		err := bcrypt.CompareHashAndPassword([]byte(hpw), []byte(pw))
		if err != nil {
			return false
		}
		return true
	}
}

func EncryptPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}
func CompareHashAndPassword(hpw string, pw string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hpw), []byte(pw))
	if err != nil {
		return false
	}
	return true
}

func BasicAuthHandler(h http.Handler) http.Handler {
	f := func(w http.ResponseWriter, req *http.Request) {
		if Authorised == nil {
			h.ServeHTTP(w, req)
			return
		}
		u, pw, ok := req.BasicAuth()
		if !ok || !Authorised(u, pw) {
			w.Header().Set("WWW-Authenticate", "Basic")
			w.WriteHeader(401)
			return
		}
		USERNAME = u
		h.ServeHTTP(w, req)
	}

	return http.HandlerFunc(f)
}

func getInput(aInput string) {
	type Inputs struct {
		Inputs []map[string]interface{} `json:"inputs"`
	}
	rawDecodedText, err := base64.StdEncoding.DecodeString(aInput)
	var data Inputs
	if err != nil {
		log.Println("base64 error %s", err)
		err = json.Unmarshal([]byte(aInput), &data)
	} else {
		err = json.Unmarshal(rawDecodedText, &data)
	}
	if err != nil {
		log.Println("Unmarshal error %s", err)
	} else {
		INPUTS = make([]string, 0)
		NAMES = make([]string, 0)
		for i := range data.Inputs {
			if value, exist := data.Inputs[i]["input"]; exist {
				INPUTS = append(INPUTS, value.(string))
			}
			if value, exist := data.Inputs[i]["type"]; exist {
				STYPES = append(STYPES, value.(string))
			}
			if value, exist := data.Inputs[i]["name"]; exist {
				NAMES = append(NAMES, value.(string))
			}
			if value, exist := data.Inputs[i]["expect"]; exist {
				EXPECTS = append(EXPECTS, value.(string))
			} else {
				EXPECTS = append(EXPECTS, "")
			}
			if value, exist := data.Inputs[i]["exec"]; exist {
				EXECS = append(EXECS, value.(string))
			} else {
				EXECS = append(EXECS, "")
			}
			if value, exist := data.Inputs[i]["receivers"]; exist {
				EXECRECEIVERS = append(EXECRECEIVERS, value.(string))
			} else {
				EXECRECEIVERS = append(EXECRECEIVERS, "")
			}
			if value, exist := data.Inputs[i]["timeout"]; exist {
				intVar, _ := strconv.Atoi(value.(string))
				TIMEOUTS = append(TIMEOUTS, intVar)
			} else {
				TIMEOUTS = append(TIMEOUTS, G_TIMEOUT)
			}
		}
	}
}

// ////////////////////////////////////////////////////////////////////////////////////////////////////
// 2 ways to run
//   - 1st: mcall web
//     call from browser: http://localhost:3000/main/core/1418,1419,2502,2694,2932,2933,2695
//   - 2nd: mcall on console
//     mcall -i="ls -al"
//
// ////////////////////////////////////////////////////////////////////////////////////////////////////
func main() {
	if len(os.Args) < 2 {
		fmt.Println("No parameter!")
		return
	}

	////[ argument ]////////////////////////////////////////////////////////////////////////////////
	var (
		help = flag.Bool("help", false, "Show these options")
		vt   = flag.String("t", "cmd", "Type")
		vi   = flag.String("i", "", "input")
		vc   = flag.String("c", "", "configuration file path")
		vw   = flag.Bool("w", false, "run webserver")
		vp   = flag.String("p", "3000", "webserver port")
		vf   = flag.String("f", "json", "return format")
		ve   = flag.String("e", "", "return result with encoding")
		vn   = flag.String("n", "", "request name")
		vlf  = flag.String("logfile", "./mcall.log", "Logfile destination. STDOUT | STDERR or file path")
		vll  = flag.String("loglevel", "DEBUG", "Loglevel CRITICAL, ERROR, WARNING, NOTICE, INFO, DEBUG")
		vec  = flag.String("ec", "", "encrypt string")
		vdc  = flag.String("dc", "", "decrypt string")
	)
	flag.Parse()
	var args = Args{"help": *help, "t": *vt, "i": *vi, "c": *vc, "w": *vw, "p": *vp, "f": *vf, "e": *ve, "n": *vn, "logfile": *vlf, "loglevel": *vll, "ec": *vec, "dc": *vdc}
	mainExec(args)
}

func sendEmail(smtpc SMTP, to string, title string, body string) {
	msg := "From: " + smtpc.user + "\n" +
		"To: " + to + "\n" +
		"Subject: " + title + "\n\n" +
		body
	plain := smtpc.host[:strings.Index(smtpc.host, ":")]
	err := smtp.SendMail(smtpc.host,
		smtp.PlainAuth("", smtpc.user, smtpc.password, plain),
		smtpc.user, []string{to}, []byte(msg))
	if err != nil {
		log.Printf("smtp error: %s", err)
	}
}

// echo "https://hooks.slack.com/services/1111/1111/1111111" | base64
func sendSlack(slackc SLACK, body string) {
	//curl -X POST -H 'Content-type: application/json' --data '{"text":"build error '${APP_NAME}' - '${BUILD_URL}'"}' ${SLACK_DEVOPS}
	body = "{\"text\":\"" + strings.Replace(body, "\"", "", -1) + "\"}"
	api_url, err := base64.StdEncoding.DecodeString(slackc.api_url)
	esCmd := "curl -k -XPOST -H 'Content-Type: application/json' -d '" + body + "' " + string(api_url)
	var timestamp = strconv.FormatInt(time.Now().Unix(), 10)
	shFile := fmt.Sprintf("/tmp/slack_%s.sh", timestamp)
	esFile, err := os.OpenFile(shFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		log.Println("esFile error: %s %s", esFile, err)
	}
	log.Println("esCmd %s", esCmd)
	esFile.WriteString(esCmd)
	esFile.Close()
	doc, err := exeCmd("bash "+shFile, 30)
	if err != nil {
		log.Println("bash error: %s", err)
	}
	log.Println(doc)
	os.Remove(shFile)
}

func sendMsg(receiver string, title string, body string, out string) {
	if out != "" {
		out2, err2 := PrettyString(out)
		if err2 == nil {
			body = body + "\n" + out2
		}
	}
	for i := range RECEIVERS.RECEIVER_TYPE {
		if RECEIVERS.RECEIVER_TYPE[i]["name"] == receiver {
			channel := RECEIVERS.RECEIVER_TYPE[i]["channel"].(string)
			if strings.Index(channel, "smtp") > -1 {
				email := RECEIVERS.RECEIVER_TYPE[i]["email"].(string)
				log.Println("email %s", email)
				for j := range SMTPS.SMTP_TYPE {
					smtp_name := channel[strings.Index(channel, "/")+1:]
					if SMTPS.SMTP_TYPE[j]["name"].(string) == smtp_name {
						smtpc := SMTP{SMTPS.SMTP_TYPE[j]["host"].(string),
							SMTPS.SMTP_TYPE[j]["user"].(string),
							SMTPS.SMTP_TYPE[j]["password"].(string)}
						title = "[DevOps] Alert: " + title
						sendEmail(smtpc, email, title, body)
					}
				}
			} else if strings.Index(channel, "slack") > -1 {
				channel := RECEIVERS.RECEIVER_TYPE[i]["channel"].(string)
				log.Println("channel %s", channel)
				for j := range SLACKS.SLACK_TYPE {
					channel_name := channel[strings.Index(channel, "/")+1 : len(channel)]
					if SLACKS.SLACK_TYPE[j]["name"].(string) == channel_name {
						slackc := SLACK{SLACKS.SLACK_TYPE[j]["name"].(string),
							SLACKS.SLACK_TYPE[j]["api_url"].(string)}
						sendSlack(slackc, title+" - "+body)
					}
				}
			}
		}
	}
}

type Args map[string]interface{}

func mainExec(args Args) map[string]string {
	var (
		help = args["help"]
		vt   = args["t"] // -t: request type ex) get, post, cmd, default: cmd
		vi   = args["i"]
		// -i: request url or command, it can be multiple with comma.
		//    ex) http://localhost:8000/test, ls -al
		//    http://localhost:8000/test1, http://localhost:8000/test2
		vc  = args["c"]  // -c: configration file ex) /etc/sl-mcall/sl-mcall.conf, default: none
		vw  = args["w"]  // -w: webserver on/off ex) on, default: off
		vp  = args["p"]  // -p: webserver port ex) default: 3000
		vf  = args["f"]  // -f: return format ex) json, plain, default: json
		ve  = args["e"]  // -e: return result with encoding ex) std, url
		vn  = args["n"]  // -n: number of worker ex) default: 10
		vec = args["ec"] // -ec: encrypt string ex) ./mcall -ec=doohee.hong\!323
		vdc = args["dc"] // -dc: encrypt string ex) ./mcall -dc=aaaaaa^bbbbbb
	)
	var rslt = map[string]string{}

	if help == true {
		flag.PrintDefaults()
		os.Exit(1)
	}
	if vt != nil {
		STYPE = vt.(string)
	} else {
		STYPE = "cmd"
	}
	if vi != nil {
		INPUTS = append(INPUTS, vi.(string))
	}
	if vc != nil {
		CONFIGFILE = vc.(string)
	}
	if vw != nil {
		WEBENABLED = vw.(bool)
	}
	if vp != nil {
		HTTPPORT = vp.(string)
	} else {
		HTTPPORT = "3000"
	}
	if vf != nil {
		FORMAT = vf.(string)
	} else {
		FORMAT = "json"
	}
	if ve != nil {
		BASE64 = ve.(string)
	}
	if vn != nil {
		NAME = vn.(string)
	}
	if vec != nil && vec != "" {
		str, _ := EncryptPassword(vec.(string))
		fmt.Println(vec.(string) + ": " + str)
		return rslt
	}
	if vdc != nil && vdc != "" {
		str := vdc.(string)
		sArry := strings.Split(str, ",")
		chk := CompareHashAndPassword(sArry[0], sArry[1])
		fmt.Println(sArry[0] + " ~ " + sArry[1] + " => " + fmt.Sprint(chk))
		return rslt
	}

	////[ configuratin file ]////////////////////////////////////////////////////////////////////////////////
	if CONFIGFILE != "" {
		viper.SetConfigFile(CONFIGFILE)
		viper.SetConfigType("yaml")
		err := viper.ReadInConfig()
		if err != nil {
			fmt.Println("parse config "+CONFIGFILE+" file error: ", err)
		}

		WORKERNUM = viper.GetInt("worker.number")
		WEBENABLED = viper.GetBool("webserver.enable")

		FORMAT = viper.GetString("response.format")
		BASE64 = viper.GetString("response.encoding.type")

		SUBJECT = viper.GetString("request.subject")
		G_TIMEOUT = viper.GetInt("request.timeout")
		if WEBENABLED == true {
			HTTPHOST = viper.GetString("webserver.host")
			HTTPPORT = viper.GetString("webserver.port")
			WEB_AUTH = viper.GetString("webserver.auth")
		} else {
			input := viper.GetString("request.input")
			STYPE = viper.GetString("request.type")
			NAME = viper.GetString("request.name")
			getInput(input)
		}
		if SUBJECT == "alert-manager" {
			ESCONF = ES{viper.GetString("request.es.host"),
				viper.GetString("request.es.id"),
				viper.GetString("request.es.password"),
				viper.GetString("request.es.index_name")}
		} else {
			ESCONF = ES{viper.GetString("response.es.host"),
				viper.GetString("response.es.id"),
				viper.GetString("response.es.password"),
				viper.GetString("response.es.index_name")}
		}
	}

	HOSTNAME, _ = os.Hostname()

	log.Println("WORKERNUM: ", WORKERNUM)
	log.Println("STYPE: ", STYPE)
	log.Println("WEBENABLED: ", WEBENABLED)
	log.Println("HTTPHOST: ", HTTPHOST)
	log.Println("HTTPPORT: ", HTTPPORT)

	////[ run app ]////////////////////////////////////////////////////////////////////////////////
	if WEBENABLED == true {
		if SUBJECT == "alert-manager" {
			CRONJOB = viper.GetString("request.cronjob")
			if CRONJOB != "" {
				rule := viper.GetString("request.rule")
				err := json.Unmarshal([]byte(rule), &RULES)
				if err != nil {
					log.Println("Unmarshal error %s", err)
				}
				smtp := viper.GetString("response.smtp")
				err = json.Unmarshal([]byte(smtp), &SMTPS)
				if err != nil {
					log.Println("Unmarshal error %s", err)
				}
				slack := viper.GetString("response.slack")
				err = json.Unmarshal([]byte(slack), &SLACKS)
				if err != nil {
					log.Println("Unmarshal error %s", err)
				}
				receiver := viper.GetString("response.receiver")
				err = json.Unmarshal([]byte(receiver), &RECEIVERS)
				if err != nil {
					log.Println("Unmarshal error %s", err)
				}
			}
			c := cron.New()
			//adminPassword := "elastic:T1zone!323" // +
			adminPassword := ESCONF.id + ":" + ESCONF.password // ":T1zone!323" // +
			esUrl := ESCONF.host
			//	curl -k -XPOST -u 'elastic:T1zone!323' \
			//https://es.elk.eks-main-t.seerslab.io/mcall_data/_search -H 'Content-Type: application/json' -d '{"_source": false, "query": {"range": {"timestamp": {"gte": "2022-05-30T03:09:28.215Z", "lte": "2022-05-31T03:09:28.215Z", "format": "strict_date_optional_time"}}}}'
			now := time.Now().UTC()
			indexName := RULES.ES_RULE[0]["index"].(string) + "-" + now.Format("2006.01.02")
			esCmd := esUrl + "/" + indexName + "/_search"
			log.Println("=========================================1")
			c.AddFunc(CRONJOB, func() {
				interval := RULES.ES_RULE[0]["interval"].(float64)
				now := time.Now().UTC()
				from := now.Add(-time.Minute * time.Duration(int(interval)))
				q_from := from.Format("2006-01-02T15:04:05.000")
				q_to := now.Format("2006-01-02T15:04:05.000")
				log.Println("interval %n", interval)
				esQeury := RULES.ES_RULE[0]["query"].(string)
				esQeury = strings.Replace(esQeury, "'", "\"", -1)
				esQeury = strings.Replace(esQeury, "${q_from}", q_from, -1)
				esQeury = strings.Replace(esQeury, "${q_to}", q_to, -1)
				log.Println("curl -k -XPOST -H 'Content-Type: application/json' -u '" + adminPassword + "' " + esCmd + " -d '" + esQeury + "'")
				out, err := exec.Command("curl", "-k", "-XPOST", "-H", "Content-Type: application/json",
					"-u", adminPassword, esCmd,
					"-d", esQeury).Output()
				if err != nil {
					log.Fatal(err)
				}
				type Outputs struct {
					Outputs map[string]map[string]int `json:"hits"`
				}
				var data Outputs
				err = json.Unmarshal(out, &data)
				log.Println(data)
				total := data.Outputs["total"]["value"]
				log.Println(total)
				if total > 0 {
					title := ""
					if RULES.ES_RULE != nil {
						title = RULES.ES_RULE[0]["name"].(string)
					}
					receivers := RULES.ES_RULE[0]["receivers"].(string)
					log.Println("receivers %s", receivers)
					body := esCmd + " " + esQeury
					receiverArry := strings.Split(receivers, ",")
					for i := range receiverArry {
						var prettyJSON bytes.Buffer
						error := json.Indent(&prettyJSON, out, "", "\t")
						if error != nil {
							log.Println("JSON parse error: ", error)
						} else {
							sendMsg(receiverArry[i], title, body, prettyJSON.String())
						}
					}
				}
			})
			c.Start()
		}
		webserver()
	} else {
		smtp := viper.GetString("response.smtp")
		err := json.Unmarshal([]byte(smtp), &SMTPS)
		if err != nil {
			log.Println("Unmarshal error %s", err)
		}
		slack := viper.GetString("response.slack")
		err = json.Unmarshal([]byte(slack), &SLACKS)
		if err != nil {
			log.Println("Unmarshal error %s", err)
		}
		receiver := viper.GetString("response.receiver")
		err = json.Unmarshal([]byte(receiver), &RECEIVERS)
		if err != nil {
			log.Println("Unmarshal error %s", err)
		}
		if SUBJECT == "daemon" {
			makeResponse(-1)
		} else {
			makeResponse(30)
		}
	}
	return rslt
}
