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

.PHONY: all build clean lint test bump-version snapshot run docker-build docker-run setup

all: build

build:
	$(GO) build -ldflags "-X 'main.version=dev' -X 'main.commit=$(COMMIT)' -X 'main.date=$(DATE)' -X 'main.metricsDataset=$(METRICS_DATASET)' -X 'main.metricsAPIToken=$(METRICS_API_TOKEN)'" -o $(BINARY_NAME) ./

clean:
	rm -f $(BINARY_NAME)

lint:
	golangci-lint run

test:
	$(GO) test -v ./...

# Bump version with pre-flight check that release notes exist.
# To preview: cz bump --dry-run
bump-version:
	@TAG=$$(cz bump --dry-run 2>&1 | grep -oE 'tag to create: v[^ ]+' | sed 's/tag to create: //'); \
	if [ -z "$$TAG" ]; then echo "Could not determine next version from cz bump --dry-run"; exit 1; fi; \
	if [ ! -f "release-notes/$$TAG.md" ]; then \
		echo ""; \
		echo "ERROR: Release notes not found for $$TAG"; \
		echo ""; \
		echo "  Missing: release-notes/$$TAG.md"; \
		echo ""; \
		echo "Before bumping, create the release notes file in a PR:"; \
		echo ""; \
		echo "  cp release-notes/TEMPLATE.md release-notes/$$TAG.md"; \
		echo ""; \
		echo "See docs/specs/release-notes-process.md for the full checklist."; \
		exit 1; \
	fi; \
	echo "Release notes found: release-notes/$$TAG.md"; \
	cz bump && git push && git push --tags

snapshot:
	METRICS_DATASET= METRICS_API_TOKEN= goreleaser release --snapshot --skip=docker --clean

run: build
	./$(BINARY_NAME) gateway start

docker-build:
	./developer/scripts/buildLocalDockerImage.sh

docker-run: docker-build
	./developer/scripts/runLocalDockerImage.sh

setup:
	./developer/scripts/install-hooks.sh