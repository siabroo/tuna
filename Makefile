# Image URL to use all building/pushing image targets
IMG ?= ghcr.io/siabroo/tuna:dev

# Tool versions are pinned via go.mod
CONTROLLER_GEN := go run sigs.k8s.io/controller-tools/cmd/controller-gen
SETUP_ENVTEST  := go run sigs.k8s.io/controller-runtime/tools/setup-envtest

# Envtest k8s control plane version
ENVTEST_K8S_VERSION ?= 1.30.0
ENVTEST_ASSETS := $(shell pwd)/testbin

.PHONY: help
help: ## Show this help.
	@awk 'BEGIN{FS=":.*##"} /^[a-zA-Z_-]+:.*##/ { printf "  %-22s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: generate
generate: ## Generate DeepCopy etc.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

.PHONY: manifests
manifests: ## Generate CRD manifests.
	$(CONTROLLER_GEN) crd:allowDangerousTypes=true paths="./api/..." output:crd:artifacts:config=config/crd/bases

.PHONY: build
build: generate ## Build the manager binary.
	go build -o bin/manager ./cmd/manager

.PHONY: test
test: generate manifests envtest ## Run unit + envtest tests.
	KUBEBUILDER_ASSETS="$(shell $(SETUP_ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir=$(ENVTEST_ASSETS) -p path)" \
		go test -race ./...

.PHONY: envtest
envtest: ## Download envtest binaries.
	$(SETUP_ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir=$(ENVTEST_ASSETS)

.PHONY: lint
lint: ## Run golangci-lint.
	go run github.com/golangci/golangci-lint/cmd/golangci-lint run ./...

KUSTOMIZE := go run sigs.k8s.io/kustomize/kustomize/v5

.PHONY: install-crd
install-crd: manifests ## Install CRDs into the current cluster.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall-crd
uninstall-crd: ## Remove CRDs.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found -f -

.PHONY: deploy
deploy: manifests ## Deploy operator to current cluster.
	cd config/manager && $(KUSTOMIZE) edit set image ghcr.io/siabroo/tuna=$(IMG)
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Tear down operator.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found -f -

.PHONY: release-manifest
release-manifest: manifests ## Build a flat release manifest at dist/install.yaml.
	cd config/manager && $(KUSTOMIZE) edit set image ghcr.io/siabroo/tuna=$(IMG)
	mkdir -p dist
	$(KUSTOMIZE) build config/default > dist/install.yaml

.PHONY: docker-build
docker-build: ## Build the container image.
	docker build -t $(IMG) .

.PHONY: docker-push
docker-push: ## Push the container image.
	docker push $(IMG)

KIND_CLUSTER ?= tuna-e2e

.PHONY: e2e-up
e2e-up: ## Spin up kind + kube-prom-stack + tuna + sample-go-app
	kind create cluster --name $(KIND_CLUSTER) --config testdata/kind-config.yaml
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts || true
	helm repo update
	helm install kube-prom prometheus-community/kube-prometheus-stack \
		--namespace monitoring --create-namespace \
		--set grafana.enabled=false --wait
	$(MAKE) docker-build IMG=tuna:e2e
	kind load docker-image tuna:e2e --name $(KIND_CLUSTER)
	cd test/samples/sample-go-app && docker build -t sample-go-app:dev . && cd ../../..
	kind load docker-image sample-go-app:dev --name $(KIND_CLUSTER)
	$(MAKE) deploy IMG=tuna:e2e
	kubectl apply -f test/samples/sample-go-app/service.yaml
	kubectl apply -f test/samples/sample-go-app/servicemonitor.yaml
	kubectl apply -f test/samples/sample-go-app/deployment.yaml
	@echo "Waiting 5min for samples to accumulate..."
	@sleep 300

.PHONY: e2e
e2e: ## Run end-to-end test against an already-up kind cluster.
	go test -race -count=1 -tags=e2e ./test/e2e/...

.PHONY: e2e-down
e2e-down: ## Delete the kind cluster.
	kind delete cluster --name $(KIND_CLUSTER)
