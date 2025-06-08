# Makefile for MCP Security Proxy

BINARY_NAME = mcp-security-proxy
GO = /usr/local/go/bin/go

.PHONY: all build clean

all: build

build:
	$(GO) build -o $(BINARY_NAME) ./cmd/mcp-security-proxy

clean:
	rm -f $(BINARY_NAME) 