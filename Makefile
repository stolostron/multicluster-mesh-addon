.DEFAULT_GOAL := default
default: all

GIT_COMMIT ?= $(shell git rev-parse --short "HEAD^{commit}" 2>/dev/null)
GIT_VERSION ?= $(shell git describe --long --tags --abbrev=7 --match 'v[0-9]*' --dirty 2>/dev/null || echo 'v0.0.0-unknown-$(GIT_COMMIT)')
BUILD_DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

BIN_DIR := $(CURDIR)/bin
CONTAINER_ENGINE ?= $(shell which podman 2>/dev/null || which docker 2>/dev/null)
GO ?= go

# Ensure bin directory exists
$(BIN_DIR):
	mkdir -p $(BIN_DIR)

# Build ldflags for version injection
LDFLAGS := -X github.com/stolostron/multicluster-mesh-addon/pkg/version.versionFromGit=$(GIT_VERSION)
LDFLAGS += -X github.com/stolostron/multicluster-mesh-addon/pkg/version.commitFromGit=$(GIT_COMMIT)
LDFLAGS += -X github.com/stolostron/multicluster-mesh-addon/pkg/version.buildDate=$(BUILD_DATE)

# Go 1.24 tool handling, from the cache; the double invocation will be fixed in 1.25
# See https://github.com/golang/go/issues/72824
gotool = $(shell $(GO) -C $(1) tool -n $(2) > /dev/null && $(GO) -C $(1) tool -n $(2))

GOLANGCI_LINT ?= $(call gotool,tools,golangci-lint)
CONTROLLER_GEN = $(call gotool,tools,controller-gen)

# Test variables
CONTROLLER_RUNTIME_BRANCH ?= release-0.23
ENVTEST_K8S_VERSION ?= 1.35
ENVTEST ?= $(BIN_DIR)/setup-envtest
TEST_CRD_DIR := $(CURDIR)/test/integration/crds

$(ENVTEST): $(BIN_DIR)
	@test -s $(ENVTEST) || GOBIN=$(BIN_DIR) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(CONTROLLER_RUNTIME_BRANCH)

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

.PHONY: gen-code
gen-code: ## Generate DeepCopy code
	$(CONTROLLER_GEN) object paths=./pkg/apis/...

.PHONY: gen-crds
gen-crds: ## Generate CRD manifests
	$(CONTROLLER_GEN) crd paths=./pkg/apis/... output:crd:dir=./config/crd

.PHONY: gen
gen: gen-code gen-crds ## Generate code and CRDs

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
test: ## Run unit tests
	go test -short ./pkg/...

.PHONY: update-test-crds
update-test-crds: ## Update test CRDs from OCM API and cert-manager dependencies
	@echo "Updating test CRDs from open-cluster-management.io/api..."
	@OCM_API_PATH=$$(go list -m -f '{{.Dir}}' open-cluster-management.io/api 2>/dev/null); \
	if [ -z "$$OCM_API_PATH" ]; then \
		echo "Error: open-cluster-management.io/api not found in go.mod"; \
		echo "Run: go mod download open-cluster-management.io/api"; \
		exit 1; \
	fi; \
	mkdir -p $(TEST_CRD_DIR)/ocm; \
	echo "Copying CRDs from $$OCM_API_PATH..."; \
	cp -v $$OCM_API_PATH/cluster/v1/*.crd.yaml $(TEST_CRD_DIR)/ocm/ 2>/dev/null || true; \
	cp -v $$OCM_API_PATH/cluster/v1beta2/*.crd.yaml $(TEST_CRD_DIR)/ocm/ 2>/dev/null || true; \
	cp -v $$OCM_API_PATH/work/v1/*.crd.yaml $(TEST_CRD_DIR)/ocm/ 2>/dev/null || true; \
	echo "Test CRDs updated successfully in $(TEST_CRD_DIR)/ocm/"
	@echo "Updating test CRDs from cert-manager..."
	@CERTMANAGER_PATH=$$(go list -m -f '{{.Dir}}' github.com/cert-manager/cert-manager 2>/dev/null); \
	if [ -z "$$CERTMANAGER_PATH" ]; then \
		echo "Error: github.com/cert-manager/cert-manager not found in go.mod"; \
		echo "Run: go mod download github.com/cert-manager/cert-manager"; \
		exit 1; \
	fi; \
	mkdir -p $(TEST_CRD_DIR)/cert-manager; \
	echo "Copying CRDs from $$CERTMANAGER_PATH..."; \
	cp -v $$CERTMANAGER_PATH/deploy/crds/cert-manager.io_certificates.yaml $(TEST_CRD_DIR)/cert-manager/ 2>/dev/null || true; \
	cp -v $$CERTMANAGER_PATH/deploy/crds/cert-manager.io_issuers.yaml $(TEST_CRD_DIR)/cert-manager/ 2>/dev/null || true; \
	echo "Test CRDs updated successfully in $(TEST_CRD_DIR)/cert-manager/"

.PHONY: test-integration
test-integration: $(ENVTEST) gen-crds update-test-crds ## Run integration tests
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" \
	go run github.com/onsi/ginkgo/v2/ginkgo -v --tags=integration ./test/integration/...

.PHONY: build
build: $(BIN_DIR) ## Build addon binary
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
