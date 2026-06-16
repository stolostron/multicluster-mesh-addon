PLUGIN_NAME ?= ossm-acm
PLUGIN_NAMESPACE ?= ossm-acm-plugin

OC ?= oc

.PHONY: build deploy teardown help

build: ## Build the frontend plugin
	npm install
	npm run build

deploy: ## Deploy (or redeploy) the frontend plugin to the cluster
	@$(OC) create namespace $(PLUGIN_NAMESPACE) --dry-run=client -o yaml | $(OC) apply -f -
	@$(OC) delete configmap $(PLUGIN_NAMESPACE)-dist -n $(PLUGIN_NAMESPACE) --ignore-not-found
	@$(OC) create configmap $(PLUGIN_NAMESPACE)-dist \
		--from-file=dist/ \
		-n $(PLUGIN_NAMESPACE)
	@$(OC) delete configmap $(PLUGIN_NAMESPACE)-nginx -n $(PLUGIN_NAMESPACE) --ignore-not-found
	@$(OC) create configmap $(PLUGIN_NAMESPACE)-nginx \
		--from-file=nginx.conf=deploy/nginx.conf \
		-n $(PLUGIN_NAMESPACE)
	@$(OC) apply -f deploy/deployment.yaml
	@$(OC) apply -f deploy/consoleplugin.yaml
	@plugins=$$($(OC) get console.operator.openshift.io cluster -o jsonpath='{.spec.plugins}' 2>/dev/null); \
	if [ -z "$$plugins" ]; then \
		$(OC) patch console.operator.openshift.io cluster \
			--type=merge \
			--patch='{"spec":{"plugins":["$(PLUGIN_NAME)"]}}'; \
	elif ! echo "$$plugins" | grep -q '$(PLUGIN_NAME)'; then \
		$(OC) patch console.operator.openshift.io cluster \
			--type=json \
			--patch='[{"op":"add","path":"/spec/plugins/-","value":"$(PLUGIN_NAME)"}]'; \
	fi
	@$(OC) rollout restart deployment/$(PLUGIN_NAMESPACE) -n $(PLUGIN_NAMESPACE)
	@$(OC) rollout status deployment/$(PLUGIN_NAMESPACE) -n $(PLUGIN_NAMESPACE) --timeout=120s
	@$(OC) rollout restart deployment/console -n openshift-console
	@$(OC) rollout status deployment/console -n openshift-console --timeout=120s

teardown: ## Remove the frontend plugin from the cluster
	@$(OC) get console.operator.openshift.io cluster -o json | \
		jq '.spec.plugins |= (. // [] | map(select(. != "$(PLUGIN_NAME)")))' | \
		$(OC) apply -f -
	@$(OC) delete -f deploy/consoleplugin.yaml --ignore-not-found
	@$(OC) delete -f deploy/deployment.yaml --ignore-not-found
	@$(OC) delete namespace $(PLUGIN_NAMESPACE) --ignore-not-found
	@$(OC) rollout restart deployment/console -n openshift-console
	@$(OC) rollout status deployment/console -n openshift-console --timeout=120s

help: ## Display this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
