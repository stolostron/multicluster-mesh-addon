# Not overridable — these values are hardcoded in deploy/*.yaml
PLUGIN_NAME := ossm-acm
PLUGIN_NAMESPACE := ossm-acm-plugin

OC ?= oc
CONTAINER_ENGINE ?= $(shell which podman 2>/dev/null || which docker 2>/dev/null)
INTERNAL_REGISTRY := image-registry.openshift-image-registry.svc:5000

# Override IMG to push to an external registry (e.g. make build IMG=quay.io/myorg/ossm-acm-console-plugin:v1)
REGISTRY = $(shell $(OC) get image.config.openshift.io/cluster -o jsonpath='{.status.externalRegistryHostnames[0]}' 2>/dev/null)
IMG ?= $(REGISTRY)/$(PLUGIN_NAMESPACE)/ossm-acm-console-plugin:latest
INTERNAL_IMG = $(INTERNAL_REGISTRY)/$(PLUGIN_NAMESPACE)/ossm-acm-console-plugin:latest

.PHONY: help build clean deploy install-deps push teardown test

##@ Testing

test: node_modules ## Run unit tests
	npm test

##@ Build & Deploy

node_modules: package.json package-lock.json
	npm install
	@touch node_modules

install-deps: node_modules ## Install npm dependencies

clean: ## Remove build artifacts and installed dependencies
	rm -rf dist node_modules

build: ## Build the container image (includes npm ci + webpack inside the container)
	@if [ -z "$(CONTAINER_ENGINE)" ]; then echo "Error: no container engine found (podman or docker)"; exit 1; fi
	$(CONTAINER_ENGINE) build -t $(IMG) .

push: ## Push the container image to the registry (default: OpenShift internal registry)
ifeq ($(origin IMG),command line)
	$(CONTAINER_ENGINE) push $(IMG)
else
	@if [ -z "$(REGISTRY)" ] || [ "$(REGISTRY)" = "<none>" ]; then \
		echo "Error: cannot detect OpenShift registry. Ensure you are logged in via 'oc login' and the registry is exposed."; \
		echo "To push to an external registry, override IMG: make push IMG=quay.io/myorg/ossm-acm-console-plugin:v1"; \
		exit 1; \
	fi
	@$(OC) create namespace $(PLUGIN_NAMESPACE) --dry-run=client -o yaml | $(OC) apply -f -
	$(CONTAINER_ENGINE) push --tls-verify=false $(IMG)
endif

deploy: push ## Push the image and deploy the plugin to the cluster
ifeq ($(origin IMG),command line)
	@$(OC) create namespace $(PLUGIN_NAMESPACE) --dry-run=client -o yaml | $(OC) apply -f -
	@sed 's|IMAGE_PLACEHOLDER|$(IMG)|g' deploy/prod-deployment.yaml | $(OC) apply -f -
else
	@sed 's|IMAGE_PLACEHOLDER|$(INTERNAL_IMG)|g' deploy/prod-deployment.yaml | $(OC) apply -f -
endif
	@$(OC) apply -f deploy/consoleplugin.yaml
	@plugins=$$($(OC) get console.operator.openshift.io cluster -o jsonpath='{.spec.plugins}' 2>/dev/null); \
	if [ -z "$$plugins" ]; then \
		$(OC) patch console.operator.openshift.io cluster \
			--type=merge \
			--patch='{"spec":{"plugins":["$(PLUGIN_NAME)"]}}'; \
	elif ! echo "$$plugins" | jq -e 'index("$(PLUGIN_NAME)")' >/dev/null 2>&1; then \
		$(OC) patch console.operator.openshift.io cluster \
			--type=json \
			--patch='[{"op":"add","path":"/spec/plugins/-","value":"$(PLUGIN_NAME)"}]'; \
	fi
	@$(OC) rollout restart deployment/$(PLUGIN_NAMESPACE) -n $(PLUGIN_NAMESPACE)
	@$(OC) rollout status deployment/$(PLUGIN_NAMESPACE) -n $(PLUGIN_NAMESPACE) --timeout=120s
	@$(OC) rollout restart deployment/console -n openshift-console
	@$(OC) rollout status deployment/console -n openshift-console --timeout=120s

teardown: ## Remove the plugin deployment from the cluster
	@$(OC) get console.operator.openshift.io cluster -o json | \
		jq '.spec.plugins |= (. // [] | map(select(. != "$(PLUGIN_NAME)")))' | \
		$(OC) apply -f -
	@$(OC) delete -f deploy/consoleplugin.yaml --ignore-not-found
	@$(OC) delete -f deploy/prod-deployment.yaml --ignore-not-found
	@$(OC) delete namespace $(PLUGIN_NAMESPACE) --ignore-not-found
	@$(OC) rollout restart deployment/console -n openshift-console
	@$(OC) rollout status deployment/console -n openshift-console --timeout=120s

##@ Help

help: ## Display available targets with descriptions
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
