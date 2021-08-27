CNI_MOUNT_PATH ?= /opt/cni/bin

IMAGE_NAME ?= k8s-ovn-cni
IMAGE_REGISTRY ?= docker.io/maiqueb
IMAGE_PULL_POLICY ?= Always
IMAGE_TAG ?= latest

IMAGE_NAME_NODE ?= ovn-cni-node

NAMESPACE ?= kube-system

TARGETS = \
	goimports-format \
	goimports-check \
	whitespace-format \
	whitespace-check \
	vet

# tools
GITHUB_RELEASE ?= $(GOBIN)/github-release

# Make does not offer a recursive wildcard function, so here's one:
rwildcard=$(wildcard $1$2) $(foreach d,$(wildcard $1*),$(call rwildcard,$d/,$2))

# Gather needed source files and directories to create target dependencies
directories=$(filter-out ./ ./vendor/ ./_out/ ./_kubevirtci/ ,$(sort $(dir $(wildcard ./*/))))
all_sources=$(call rwildcard,$(directories),*) $(filter-out $(TARGETS), $(wildcard *))
go_sources=$(call rwildcard,cmd/,*.go) $(call rwildcard,pkg/,*.go) $(call rwildcard,tests/,*.go)

# Configure Go
export GOOS=linux
export GOARCH=amd64
export CGO_ENABLED=0
export GO111MODULE=on
export GOFLAGS=-mod=vendor

BIN_DIR = $(CURDIR)/build/_output/bin/
export GOROOT=$(BIN_DIR)/go/
export GOBIN = $(GOROOT)/bin/
export PATH := $(GOBIN):$(PATH)

GO := $(GOBIN)/go

$(GO):
	hack/install-go.sh $(BIN_DIR)

.ONESHELL:

build-cni: $(GO)
	$(GO) build -o build/ovn-cni github.com/maiqueb/ovn-cni/cmd/cni/server

build-controller: $(GO)
	$(GO) build -o build/k8s-ovn-controller github.com/maiqueb/ovn-cni/cmd/controller

format:
	$(GO)fmt -w ./pkg/ ./cmd/

vet: $(go_sources) $(GO)
	$(GO) vet ./pkg/... ./cmd/...

docker-build:
	docker build -t ${IMAGE_REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG} .
	docker build -t ${IMAGE_REGISTRY}/${IMAGE_NAME_NODE}:${IMAGE_TAG} -f ./cmd/cni/Dockerfile .

docker-push: docker-build
	docker push ${IMAGE_REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}
	docker push ${IMAGE_REGISTRY}/${IMAGE_NAME_NODE}:${IMAGE_TAG}

docker-tag-latest: docker-build
	docker tag ${IMAGE_REGISTRY}/${IMAGE_NAME}:latest ${IMAGE_REGISTRY}/${IMAGE_NAME}:${IMAGE_TAG}
	docker tag ${IMAGE_REGISTRY}/${IMAGE_NAME_NODE}:latest ${IMAGE_REGISTRY}/${IMAGE_NAME_NODE}:${IMAGE_TAG}

vendor: $(GO)
	$(GO) mod tidy
	$(GO) mod vendor

.PHONY: \
	all \
	docker-build \
	docker-push \
	format \
	vendor
