# Not overridable — these values are hardcoded in deploy/*.yaml
PLUGIN_NAME := ossm-acm
PLUGIN_NAMESPACE := ossm-acm-plugin

OC ?= oc
CONTAINER_ENGINE ?= $(shell which podman 2>/dev/null || which docker 2>/dev/null)
INTERNAL_REGISTRY := image-registry.openshift-image-registry.svc:5000

# REGISTRY and IMG are lazily evaluated so dev-* targets don't require the image registry to be exposed.
# Override IMG to push to an external registry (e.g. make prod-build IMG=quay.io/myorg/ossm-acm-console-plugin:v1)
REGISTRY = $(shell $(OC) get image.config.openshift.io/cluster -o jsonpath='{.status.externalRegistryHostnames[0]}' 2>/dev/null)
IMG ?= $(REGISTRY)/$(PLUGIN_NAMESPACE)/ossm-acm-console-plugin:latest
INTERNAL_IMG = $(INTERNAL_REGISTRY)/$(PLUGIN_NAMESPACE)/ossm-acm-console-plugin:latest

# Node 20 is required — Node 22+ has stricter ESM module resolution that breaks
# ts-node's loading of webpack.config.ts. The Dockerfile uses ubi9/nodejs-20 so
# production image builds are unaffected; this check guards local dev builds only.
REQUIRED_NODE_MAJOR := 20
NODE_MAJOR := $(shell node -v 2>/dev/null | sed 's/v\([0-9]*\).*/\1/')
define CHECK_NODE
	@if [ -z "$(NODE_MAJOR)" ]; then \
		echo "Error: node is not installed. Install Node.js $(REQUIRED_NODE_MAJOR) (use nvm, fnm, or n)."; \
		exit 1; \
	elif [ "$(NODE_MAJOR)" != "$(REQUIRED_NODE_MAJOR)" ]; then \
		echo "Error: Node.js $(REQUIRED_NODE_MAJOR) is required but you have $$(node -v). Switch with nvm/fnm/n (e.g. nvm use $(REQUIRED_NODE_MAJOR))"; \
		exit 1; \
	fi
endef

.PHONY: help dev-build dev-deploy dev-teardown prod-build prod-push prod-deploy prod-teardown test

##@ Development

dev-build: ## Compile TS/React source into dist/ for local dev
	$(CHECK_NODE)
	npm install
	npm run build

dev-deploy: ## Deploy (or redeploy) the plugin to the cluster using ConfigMaps
	@$(OC) create namespace $(PLUGIN_NAMESPACE) --dry-run=client -o yaml | $(OC) apply -f -
	@$(OC) delete configmap $(PLUGIN_NAMESPACE)-dist -n $(PLUGIN_NAMESPACE) --ignore-not-found
	@$(OC) create configmap $(PLUGIN_NAMESPACE)-dist \
		--from-file=dist/ \
		-n $(PLUGIN_NAMESPACE)
	@$(OC) delete configmap $(PLUGIN_NAMESPACE)-nginx -n $(PLUGIN_NAMESPACE) --ignore-not-found
	@$(OC) create configmap $(PLUGIN_NAMESPACE)-nginx \
		--from-file=nginx.conf=deploy/nginx.conf \
		-n $(PLUGIN_NAMESPACE)
	@$(OC) apply -f deploy/dev-deployment.yaml
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

dev-teardown: ## Remove the dev plugin deployment from the cluster
	@$(OC) get console.operator.openshift.io cluster -o json | \
		jq '.spec.plugins |= (. // [] | map(select(. != "$(PLUGIN_NAME)")))' | \
		$(OC) apply -f -
	@$(OC) delete -f deploy/consoleplugin.yaml --ignore-not-found
	@$(OC) delete -f deploy/dev-deployment.yaml --ignore-not-found
	@$(OC) delete namespace $(PLUGIN_NAMESPACE) --ignore-not-found
	@$(OC) rollout restart deployment/console -n openshift-console
	@$(OC) rollout status deployment/console -n openshift-console --timeout=120s

##@ Testing

test: ## Run unit tests
	$(CHECK_NODE)
	npm test

##@ Production

prod-build: ## Build the production container image (includes npm ci + webpack)
	@if [ -z "$(CONTAINER_ENGINE)" ]; then echo "Error: no container engine found (podman or docker)"; exit 1; fi
	$(CONTAINER_ENGINE) build -t $(IMG) .

prod-push: ## Push the container image to the registry (default: OpenShift internal registry)
ifeq ($(origin IMG),command line)
	$(CONTAINER_ENGINE) push $(IMG)
else
	@if [ -z "$(REGISTRY)" ] || [ "$(REGISTRY)" = "<none>" ]; then \
		echo "Error: cannot detect OpenShift registry. Ensure you are logged in via 'oc login' and the registry is exposed."; \
		echo "To push to an external registry, override IMG: make prod-push IMG=quay.io/myorg/ossm-acm-console-plugin:v1"; \
		exit 1; \
	fi
	# OpenShift internal registry requires the namespace to exist before accepting image pushes
	@$(OC) create namespace $(PLUGIN_NAMESPACE) --dry-run=client -o yaml | $(OC) apply -f -
	$(CONTAINER_ENGINE) push --tls-verify=false $(IMG)
endif

prod-deploy: prod-push ## Push the image and deploy the plugin using the baked image
ifeq ($(origin IMG),command line)
	# External registry: namespace wasn't created in prod-push, so create it here
	@$(OC) create namespace $(PLUGIN_NAMESPACE) --dry-run=client -o yaml | $(OC) apply -f -
	@sed 's|IMAGE_PLACEHOLDER|$(IMG)|g' deploy/prod-deployment.yaml | $(OC) apply -f -
else
	# Internal registry: namespace was already created in prod-push
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

prod-teardown: ## Remove the production plugin deployment from the cluster
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
