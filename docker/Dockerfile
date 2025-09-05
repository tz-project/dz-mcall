##
## Build
##
FROM golang:1.21-alpine AS build

WORKDIR /app

# Copy go module files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and configuration
COPY *.go ./
COPY etc/mcall.yaml ./

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o mcall .

##
## Deploy
##
FROM alpine:3.18

# Install required packages
RUN apk --no-cache add ca-certificates curl busybox-extras

WORKDIR /app

# Copy binary and configuration
COPY --from=build /app/mcall /app/mcall
COPY --from=build /app/mcall.yaml /app/mcall.yaml

# Create log directory and set permissions
RUN mkdir -p /app/log/mcall && \
    chmod 755 /app/log/mcall && \
    chmod +x /app/mcall

# Set environment variables
ENV PATH="/app:${PATH}"

# Expose default port
EXPOSE 3000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:3000/healthcheck || exit 1

# Default command with webserver enabled
CMD ["/app/mcall", "-w=true", "-p=3000", "-c=/app/mcall.yaml"]
