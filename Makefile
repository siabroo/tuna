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
