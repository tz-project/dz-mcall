# tz-mcall

[![Go Version](https://img.shields.io/badge/Go-1.18+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

A high-performance, concurrent HTTP request and command execution tool written in Go. Supports multiple input types, worker pools, and real-time monitoring.

## 🚀 Features

- Multiple Input Types: HTTP GET/POST requests, shell commands
- Concurrent Processing: Configurable worker pools for high throughput
- Real-time Monitoring: Web interface with health checks
- Flexible Configuration: YAML configuration files with environment overrides
- Multiple Deployment Options: Docker, Kubernetes, Debian packages
- Comprehensive Logging: Structured logging with configurable levels
- Health Monitoring: Built-in health check endpoints

## 📋 Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Usage](#usage)
- [API Reference](#api-reference)
- [Deployment](#deployment)
- [Development](#development)
- [Monitoring](#monitoring)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)

## 🛠️ Installation

### Prerequisites

- Go 1.18 or higher
- Docker (optional, for containerized deployment)
- Kubernetes cluster (optional, for K8s deployment)

### From Source

```bash
# Clone the repository
git clone https://github.com/doohee323/tz-mcall.git
cd tz-mcall

# Install dependencies
go mod tidy
go mod vendor

# Build the application
go build -o mcall .

# Make executable
chmod +x mcall
```

### Using Go Modules

```bash
go get github.com/doohee323/tz-mcall
```

## 🚀 Quick Start

### Basic Command Execution

```bash
# Execute a single command
./mcall -i="ls -la"

# Execute multiple commands
./mcall -i="pwd,ls -la,echo hello"

# HTTP GET request
./mcall -t=get -i="http://localhost:3000/healthcheck"
```

### Start Web Server

```bash
# Start web server on default port 3000
./mcall -w=true

# Start web server on custom port
./mcall -w=true -p=8080
```

### Using Configuration File

```bash
# Use configuration file
./mcall -c=etc/mcall.yaml
```

## ⚙️ Configuration

### Configuration File (mcall.yaml)

```yaml
request:
  type: cmd
  input: |
    {
      "inputs": [
        {"input": "pwd"},
        {"input": "ls -la"}
      ]
    }

response:
  format: json

worker:
  number: 5

log:
  level: info
  file: /var/log/mcall/mcall.log

webserver:
  enable: true
  host: 0.0.0.0
  port: 3000
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MCALL_LOG_LEVEL` | Log level (DEBUG, INFO, ERROR) | DEBUG |
| `MCALL_WORKER_NUM` | Number of workers | 10 |
| `MCALL_HTTP_PORT` | HTTP server port | 3000 |

## 📖 Usage

### Command Line Options

| Flag | Description | Default | Example |
|------|-------------|---------|---------|
| `-i` | Input commands/URLs (comma-separated) | - | `-i="ls -la,pwd"` |
| `-t` | Request type (cmd, get, post) | cmd | `-t=get` |
| `-w` | Enable web server | false | `-w=true` |
| `-p` | Web server port | 3000 | `-p=8080` |
| `-f` | Response format (json, plain) | json | `-f=plain` |
| `-n` | Number of workers | 10 | `-n=20` |
| `-l` | Log level | debug | `-l=info` |
| `-c` | Configuration file path | - | `-c=config.yaml` |

### Examples

#### Command Execution

```bash
# Single command
./mcall -i="ls -la"

# Multiple commands
./mcall -i="pwd,ls -la,echo hello"

# With custom worker count
./mcall -i="ls -la" -n=5
```

#### HTTP Requests

```bash
# GET request
./mcall -t=get -i="http://api.example.com/status"

# POST request
./mcall -t=post -i="http://api.example.com/data"

# Multiple URLs
./mcall -t=get -i="http://api1.example.com,http://api2.example.com"
```

#### Web Server Mode

```bash
# Start web server
./mcall -w=true -p=8080

# Test via HTTP
curl "http://localhost:8080/mcall/cmd/$(echo '{"inputs":[{"input":"ls -la"}]}' | base64)"
```

## 🌐 API Reference

### Endpoints

#### Health Check
```
GET /healthcheck
```
Returns application health status.

#### Command Execution
```
GET /mcall/cmd/{base64-encoded-params}
POST /mcall
```
Execute commands with base64-encoded JSON parameters.

#### HTTP Requests
```
GET /mcall/get/{base64-encoded-params}
POST /mcall/post/{base64-encoded-params}
```
Execute HTTP GET/POST requests.

### Request Format

```json
{
  "inputs": [
    {"input": "ls -la", "name": "list-files"},
    {"input": "http://api.example.com/status", "type": "get"}
  ]
}
```

### Response Format

```json
[
  {
    "errorCode": "0",
    "input": "ls -la",
    "name": "list-files",
    "result": "total 1234\ndrwxr-xr-x...",
    "ts": "2025-08-22T23:02:08.804"
  }
]
```

## 🚀 Deployment

### Docker

#### Build Image
```bash
docker build -f docker/Dockerfile -t tz-mcall:latest .
```

#### Run Container
```bash
# Basic run
docker run -d -p 3000:3000 tz-mcall:latest

# With custom configuration
docker run -p 3000:3000 -v $(pwd)/etc/mcall.yaml:/app/mcall.yaml tz-mcall:latest
```

#### Docker Compose
```bash
cd docker
docker-compose up -d
```

### Kubernetes

#### Production Deployment
```bash
kubectl apply -f k8s/k8s.yaml -n devops
```

#### Development Deployment
```bash
kubectl apply -f k8s/k8s-dev.yaml -n devops-dev
```

#### CronJob Deployment
```bash
kubectl apply -f k8s/k8s-crontab.yaml -n devops
```

### Debian Package

#### Build Package
```bash
cd deb
./build_deb.sh
```

#### Install Package
```bash
sudo dpkg -i tz-mcall.deb
```

## 🛠️ Development

### Project Structure

```
tz-mcall/
├── mcall.go              # Main application code
├── mcall_test.go         # Test files
├── etc/                  # Configuration files
│   ├── mcall.yaml       # Main configuration
│   ├── allow_access.yaml # Access control config
│   └── block_access.yaml # Block list config
├── docker/               # Docker configuration
│   ├── Dockerfile       # Container definition
│   ├── docker-compose.yml # Multi-service setup
│   └── local.sh         # Local development script
├── k8s/                  # Kubernetes manifests
│   ├── k8s.yaml         # Production deployment
│   ├── k8s-dev.yaml     # Development deployment
│   ├── k8s-crontab.yaml # CronJob deployment
│   ├── config.sh        # Configuration script
│   └── Jenkinsfile      # CI/CD pipeline
├── deb/                  # Debian packaging
│   ├── build_deb.sh     # Package build script
│   └── pkg-build/       # Package structure
├── go.mod                # Go module definition
├── go.sum                # Go module checksums
├── glide.yaml            # Legacy dependency management
└── README.md             # This file
```

### Building

```bash
# Development build
go build -o mcall .

# Production build (Linux)
GOOS=linux GOARCH=amd64 go build -o mcall-linux .

# With debug information
go build -gcflags="-N -l" -o mcall-debug .
```

### Testing

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run specific test
go test -run TestCommandExecution

# Run with coverage
go test -cover ./...
```

### Dependency Management

#### Go Modules (Recommended)
```bash
go mod init                    # Initialize module
go mod tidy                   # Clean up dependencies
go get ./...                  # Get all dependencies
go mod vendor                 # Vendor dependencies
```

#### Glide (Legacy)
```bash
glide install                  # Install dependencies
glide update                   # Update dependencies
glide get github.com/spf13/viper  # Add new dependency
```

## 📊 Monitoring

### Health Checks

```bash
# Application health
curl http://localhost:3000/healthcheck

# Kubernetes health check
kubectl get pods -n devops -l app=tz-mcall
```

### Logging

```bash
# View application logs
tail -f /var/log/mcall/mcall.log

# View container logs
docker logs tz-mcall-container

# View Kubernetes logs
kubectl logs -f deployment/tz-mcall -n devops
```

### Metrics

- Worker pool utilization
- Request processing time
- Error rates
- Memory and CPU usage

## 🔧 Troubleshooting

### Common Issues

#### Permission Denied
```bash
# Fix log directory permissions
sudo mkdir -p /var/log/mcall
sudo chmod 755 /var/log/mcall
```

#### Port Already in Use
```bash
# Check port usage
lsof -i :3000

# Use different port
./mcall -w=true -p=3001
```

#### Configuration File Not Found
```bash
# Check file path
ls -la etc/mcall.yaml

# Use absolute path
./mcall -c=/absolute/path/to/mcall.yaml
```

### Debug Mode

```bash
# Enable debug logging
./mcall -l=debug -i="ls -la"

# Run with verbose output
./mcall -v -i="ls -la"
```

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Development Guidelines

- Follow Go coding standards
- Add tests for new features
- Update documentation
- Ensure backward compatibility

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 👥 Authors

- Dewey Hong - Initial work - [doohee323](https://github.com/doohee323)

---

**Made with ❤️ by the tz-mcall team**


