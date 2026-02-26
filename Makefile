.DEFAULT_GOAL := default
default: all

GIT_COMMIT ?= $(shell git rev-parse --short "HEAD^{commit}" 2>/dev/null)
GIT_VERSION ?= $(shell git describe --long --tags --abbrev=7 --match 'v[0-9]*' --dirty 2>/dev/null || echo 'v0.0.0-unknown-$(GIT_COMMIT)')
BUILD_DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

BIN_DIR := $(CURDIR)/bin
CONTAINER_ENGINE ?= $(shell which podman 2>/dev/null || which docker 2>/dev/null)
GO ?= go

# Build ldflags for version injection
LDFLAGS := -X github.com/stolostron/multicluster-mesh-addon/pkg/version.versionFromGit=$(GIT_VERSION)
LDFLAGS += -X github.com/stolostron/multicluster-mesh-addon/pkg/version.commitFromGit=$(GIT_COMMIT)
LDFLAGS += -X github.com/stolostron/multicluster-mesh-addon/pkg/version.buildDate=$(BUILD_DATE)

# Go 1.24 tool handling, from the cache; the double invocation will be fixed in 1.25
# See https://github.com/golang/go/issues/72824
gotool = $(shell $(GO) -C $(1) tool -n $(2) > /dev/null && $(GO) -C $(1) tool -n $(2))

GOLANGCI_LINT ?= $(call gotool,tools,golangci-lint)
CONTROLLER_GEN = $(call gotool,tools,controller-gen)

# Image registry and name
REGISTRY_BASE ?= quay.io/stolostron
IMG ?= $(REGISTRY_BASE)/multicluster-mesh-addon:$(GIT_VERSION)

.PHONY: deps
deps: go.mod go.sum
	go mod tidy
	go mod download
	go mod verify

.PHONY: fmt
fmt: ## Format Go code
	go fmt ./...

.PHONY: generate
generate: ## Run code generators
	$(CONTROLLER_GEN) object paths=./pkg/apis/...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: verify
verify: verify-gofmt vet ## Verify code passes all checks without modifying files

.PHONY: verify-gofmt
verify-gofmt: ## Verify code is formatted correctly
	@echo "Verifying gofmt..."
	@test -z "$$(gofmt -s -l . | grep -v zz_generated)" || (echo "Code not formatted, run 'make fmt'" && exit 1)

.PHONY: golangci-lint
golangci-lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run --timeout=5m ./...

.PHONY: test
test: ## Run tests
	go test ./pkg/...

.PHONY: build
build: ## Build addon binary
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -buildvcs=false -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/multicluster-mesh-addon .

.PHONY: all
all: build test

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf $(BIN_DIR)

.PHONY: images
images: ## Build the container image
	$(CONTAINER_ENGINE) build -t $(IMG) .

.PHONY: help
help: ## Display this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
