BINARY_NAME = maybe-dont
GO = go
COMMIT = $(shell git rev-parse HEAD)
DATE = $(shell date -u '+%Y-%m-%d %H:%M:%S')

# Read metrics configuration from environment variables
# Set these in your environment for local builds with metrics enabled:
#   export METRICS_DATASET=your-dataset-name
#   export METRICS_API_TOKEN=your-api-token
METRICS_DATASET ?= $(shell echo $$METRICS_DATASET)
METRICS_API_TOKEN ?= $(shell echo $$METRICS_API_TOKEN)

.PHONY: all build clean lint test

all: build

build:
	$(GO) build -ldflags "-X 'main.version=dev' -X 'main.commit=$(COMMIT)' -X 'main.date=$(DATE)' -X 'main.metricsDataset=$(METRICS_DATASET)' -X 'main.metricsAPIToken=$(METRICS_API_TOKEN)'" -o $(BINARY_NAME) ./

clean:
	rm -f $(BINARY_NAME)

lint:
	golangci-lint run

test:
	$(GO) test -v ./...

bump-version:
	cz bump
	git push
	git push --tags
snapshot:
	goreleaser release --snapshot --skip=docker --clean
