SHELL         := /usr/bin/env bash
.SHELLFLAGS   := -euo pipefail -c
.DEFAULT_GOAL := help

BINARY    := notation-aws-verifier
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)

IMAGE     ?= ghcr.io/krax1337/notation-aws-verifier
TAG       ?= dev
IMAGE_REGISTRY   := $(firstword $(subst /, ,$(IMAGE)))
IMAGE_REPOSITORY := $(patsubst $(IMAGE_REGISTRY)/%,%,$(IMAGE))

CHART_DIR ?= charts/notation-aws-verifier
RELEASE   ?= notation-aws-verifier
NAMESPACE ?= notation-aws-verifier
MANIFEST  ?= configs/install.yaml

KIND_CLUSTER    ?= notation-aws-verifier
KIND_NODE_IMAGE ?=

# Tools run through `go run pkg@version`; override any of them to use a local binary.
GOLANGCI_LINT_VERSION ?= v2.13.2
HELM_DOCS_VERSION     ?= v1.14.2
KUBECONFORM_VERSION   ?= v0.8.0
KIND_VERSION          ?= v0.33.0
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
HELM_DOCS     ?= go run github.com/norwoodj/helm-docs/cmd/helm-docs@$(HELM_DOCS_VERSION)
KUBECONFORM   ?= go run github.com/yannh/kubeconform/cmd/kubeconform@$(KUBECONFORM_VERSION)
# The default schema catalog has no CustomResourceDefinition schema; every other kind must validate.
KUBECONFORM_FLAGS ?= -strict -summary -skip CustomResourceDefinition
KIND          ?= go run sigs.k8s.io/kind@$(KIND_VERSION)
HELM          ?= helm

##@ General

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make <target>\n"} \
		/^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Go

.PHONY: build
build: ## Build the binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) .

.PHONY: test
test: ## Run unit tests with the race detector and coverage
	go test -race -coverprofile=coverage.out ./...

.PHONY: lint
lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: vulncheck
vulncheck: ## Run govulncheck (fails on reachable vulns not in .govulncheck-ignore)
	hack/govulncheck.sh

.PHONY: fmt
fmt: ## Format Go code
	go fmt ./...

##@ Container

.PHONY: docker-build
docker-build: ## Build the container image for the local platform (override IMAGE and TAG)
	docker buildx build --load --build-arg VERSION=$(VERSION) -t $(IMAGE):$(TAG) .

##@ Helm

.PHONY: helm-lint
helm-lint: ## Lint the Helm chart
	$(HELM) lint --strict $(CHART_DIR)

.PHONY: helm-docs
helm-docs: ## Regenerate the chart README with helm-docs
	cd charts && $(HELM_DOCS)

.PHONY: helm-template
helm-template: ## Render the chart with several value sets and validate with kubeconform
	$(HELM) template $(RELEASE) $(CHART_DIR) -n $(NAMESPACE) \
		| $(KUBECONFORM) $(KUBECONFORM_FLAGS)
	$(HELM) template $(RELEASE) $(CHART_DIR) -n $(NAMESPACE) \
		--set replicaCount=3 --set logFormat=json --set region=eu-west-1 \
		--set 'tokenReview.audiences={kyverno-svc.kyverno.io}' \
		| $(KUBECONFORM) $(KUBECONFORM_FLAGS)
	$(HELM) template $(RELEASE) $(CHART_DIR) -n $(NAMESPACE) \
		--set crds.install=false --set tokenReview.enabled=false --set cache.enabled=false \
		| $(KUBECONFORM) $(KUBECONFORM_FLAGS)

.PHONY: manifests
manifests: ## Regenerate configs/install.yaml from the chart
	{ printf -- '---\napiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n' '$(NAMESPACE)'; \
	  $(HELM) template $(RELEASE) $(CHART_DIR) -n $(NAMESPACE) --skip-tests | sed -e '/^# Source: /d'; \
	} > $(MANIFEST)

##@ Local e2e (kind)

.PHONY: kind-create
kind-create: ## Create a kind cluster
	$(KIND) create cluster --name $(KIND_CLUSTER) $(if $(KIND_NODE_IMAGE),--image $(KIND_NODE_IMAGE)) --wait 120s

.PHONY: kind-load
kind-load: docker-build ## Build the image and load it into the kind cluster
	$(KIND) load docker-image $(IMAGE):$(TAG) --name $(KIND_CLUSTER)

.PHONY: kind-install
kind-install: ## Install the chart into the kind cluster using the locally built image
	$(HELM) upgrade --install $(RELEASE) $(CHART_DIR) -n $(NAMESPACE) --create-namespace --wait \
		--kube-context kind-$(KIND_CLUSTER) \
		--set image.registry=$(IMAGE_REGISTRY) --set image.repository=$(IMAGE_REPOSITORY) \
		--set image.tag=$(TAG) --set image.pullPolicy=IfNotPresent

.PHONY: kind-test
kind-test: ## Run the chart's helm tests in the kind cluster
	$(HELM) test $(RELEASE) -n $(NAMESPACE) --kube-context kind-$(KIND_CLUSTER)

.PHONY: kind-e2e
kind-e2e: kind-create kind-load kind-install kind-test ## Create cluster, load image, install chart, run helm tests

.PHONY: kind-delete
kind-delete: ## Delete the kind cluster
	$(KIND) delete cluster --name $(KIND_CLUSTER)
