# Makefile for MCP Security Proxy

BINARY_NAME = maybe-dont
GO = /usr/local/go/bin/go

.PHONY: all build clean lint

all: build

build:
	$(GO) build -o $(BINARY_NAME) ./

clean:
	rm -f $(BINARY_NAME)

lint:
	golangci-lint run 