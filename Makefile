BINARY_NAME = maybe-dont
GO = /usr/local/go/bin/go
COMMIT = $(shell git rev-parse HEAD)
DATE = $(shell date -u '+%Y-%m-%d %H:%M:%S')

.PHONY: all build clean lint test

all: build

build:
	$(GO) build -ldflags "-X 'main.version=dev' -X 'main.commit=$(COMMIT)' -X 'main.date=$(DATE)'" -o $(BINARY_NAME) ./

clean:
	rm -f $(BINARY_NAME)

lint:
	golangci-lint run

test:
	$(GO) test -v ./...
bump-version:
	cz bump
snapshot:
	goreleaser release --snapshot --skip=docker --clean
