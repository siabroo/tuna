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
