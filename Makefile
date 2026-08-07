# Copyright 2026 The Platform Mesh Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

SHELL := /usr/bin/env bash

# Go parameters
GOCMD = go
GOBUILD = $(GOCMD) build
GORUN = $(GOCMD) run
GOMOD = $(GOCMD) mod
GOFMT = $(GOCMD) fmt

# hack scripts setup
TOOLS_DIR = hack/tools
export UGET_DIRECTORY = $(TOOLS_DIR)
export UGET_CHECKSUMS = hack/tools.checksums
export UGET_VERSIONED_BINARIES = true

CONTROLLER_GEN_VER := v0.17.3
CONTROLLER_GEN_BIN := controller-gen
CONTROLLER_GEN := $(abspath $(TOOLS_DIR))/$(CONTROLLER_GEN_BIN)-$(CONTROLLER_GEN_VER)
export CONTROLLER_GEN # so hack scripts can use it

KCP_APIGEN_VER := 0.32.3
KCP_APIGEN_BIN := apigen
KCP_APIGEN_GEN := $(abspath $(TOOLS_DIR))/$(KCP_APIGEN_BIN)-$(KCP_APIGEN_VER)
export KCP_APIGEN_GEN # so hack scripts can use it

YAML_PATCH_VER := v0.0.11
YAML_PATCH_BIN := yaml-patch
YAML_PATCH := $(abspath $(TOOLS_DIR))/$(YAML_PATCH_BIN)-$(YAML_PATCH_VER)
export YAML_PATCH # so hack scripts can use it

# Binary names
OPERATOR_BINARY_NAME = operator

# Build directory
BUILD_DIR = bin

# Container runtime (docker or podman; auto-detected if not set)
ifeq ($(origin CONTAINER_RUNTIME),undefined)
  CONTAINER_RUNTIME := $(shell command -v docker 2>/dev/null || command -v podman 2>/dev/null || echo docker)
endif

# Image parameters
IMAGE_REGISTRY ?= ghcr.io/platform-mesh
IMAGE_TAG ?= dev

# Operator image
OPERATOR_IMAGE_NAME ?= kbind-provider-operator
OPERATOR_IMAGE ?= $(IMAGE_REGISTRY)/$(OPERATOR_IMAGE_NAME):$(IMAGE_TAG)

# Portal image
PORTAL_IMAGE_NAME ?= kbind-provider-portal
PORTAL_IMAGE ?= $(IMAGE_REGISTRY)/$(PORTAL_IMAGE_NAME):$(IMAGE_TAG)
PORTAL_PORT ?= 4300

.PHONY: all
all: codegen build

## build: Build all binaries
.PHONY: build
build: build-operator

## build-operator: Build the operator binary
.PHONY: build-operator
build-operator: fmt vet
	$(GOBUILD) -o $(BUILD_DIR)/$(OPERATOR_BINARY_NAME) ./cmd/operator/...

tools: $(CONTROLLER_GEN) $(KCP_APIGEN_GEN) $(YAML_PATCH) ## Install tools
.PHONY: tools

$(CONTROLLER_GEN):
	@UNCOMPRESSED=true hack/uget.sh https://github.com/kubernetes-sigs/controller-tools/releases/download/{VERSION}/controller-gen-{GOOS}-{GOARCH} ${CONTROLLER_GEN_BIN} $(CONTROLLER_GEN_VER) controller-gen*

$(KCP_APIGEN_GEN):
	@hack/uget.sh https://github.com/kcp-dev/kcp/releases/download/v{VERSION}/apigen_{VERSION}_{GOOS}_{GOARCH}.tar.gz $(KCP_APIGEN_BIN) $(KCP_APIGEN_VER) bin/apigen*

$(YAML_PATCH):
	@GO_MODULE=true hack/uget.sh github.com/pivotal-cf/yaml-patch/cmd/yaml-patch $(YAML_PATCH_BIN) $(YAML_PATCH_VER)

## fmt: Run go fmt
.PHONY: fmt
fmt:
	$(GOFMT) ./...

## vet: Run go vet
.PHONY: vet
vet:
	$(GOCMD) vet ./...

## tidy: Run go mod tidy
.PHONY: tidy
tidy:
	$(GOMOD) tidy

## operator-image-build: Build operator container image locally
.PHONY: operator-image-build
operator-image-build:
	$(CONTAINER_RUNTIME) build -t $(OPERATOR_IMAGE) -f deploy/Dockerfile .

## operator-image-push: Push operator container image to registry
.PHONY: operator-image-push
operator-image-push: operator-image-build
	$(CONTAINER_RUNTIME) push $(OPERATOR_IMAGE)

## portal-image-build: Build portal container image locally
.PHONY: portal-image-build
portal-image-build:
	$(CONTAINER_RUNTIME) build -t $(PORTAL_IMAGE) -f deploy/portal.Dockerfile .

## portal-image-push: Push portal container image to registry
.PHONY: portal-image-push
portal-image-push: portal-image-build
	$(CONTAINER_RUNTIME) push $(PORTAL_IMAGE)

## images: Build all container images
.PHONY: images
images: operator-image-build portal-image-build

## images-push: Push all container images
.PHONY: images-push
images-push: operator-image-push portal-image-push

# Kind cluster parameters
KIND_CLUSTER ?= platform-mesh

## kind-load-operator: Load operator image into kind cluster
.PHONY: kind-load-operator
kind-load-operator:
	$(CONTAINER_RUNTIME) save $(OPERATOR_IMAGE) | kind load image-archive /dev/stdin --name $(KIND_CLUSTER)

## kind-load-portal: Load portal image into kind cluster
.PHONY: kind-load-portal
kind-load-portal:
	$(CONTAINER_RUNTIME) save $(PORTAL_IMAGE) | kind load image-archive /dev/stdin --name $(KIND_CLUSTER)

## kind-load-all: Load all images into kind cluster
.PHONY: kind-load-all
kind-load-all: kind-load-operator kind-load-portal

## portal-run: Run portal container locally (accessible at http://localhost:$(PORTAL_PORT))
.PHONY: portal-run
portal-run:
	$(CONTAINER_RUNTIME) run --rm -p $(PORTAL_PORT):8080 $(PORTAL_IMAGE)

## portal-run-detached: Run portal container in background
.PHONY: portal-run-detached
portal-run-detached:
	$(CONTAINER_RUNTIME) run -d --rm --name kbind-provider-portal -p $(PORTAL_PORT):8080 $(PORTAL_IMAGE)
	@echo "Portal running at http://localhost:$(PORTAL_PORT)"
	@echo "Stop with: $(CONTAINER_RUNTIME) stop kbind-provider-portal"

## portal-stop: Stop the portal container
.PHONY: portal-stop
portal-stop:
	$(CONTAINER_RUNTIME) stop kbind-provider-portal

# Refresh the operator chart's bundled CRDs from the generated sdk CRDs.
.PHONY: helm-sync-crds
helm-sync-crds: codegen
	cp sdk/config/crd/kbind-provider.platform-mesh.io_*.yaml $(OPERATOR_CHART)/crds/

.PHONY: codegen
codegen: $(CONTROLLER_GEN) $(KCP_APIGEN_GEN) $(YAML_PATCH)
	cd sdk && $(CONTROLLER_GEN) object paths=./apis/...
	cd sdk && $(CONTROLLER_GEN) crd paths=./apis/... output:crd:dir=./config/crd
	rm -f sdk/config/crd/_.yaml
	$(KCP_APIGEN_GEN) --input-dir sdk/config/crd --output-dir config/provider
	@for f in config/provider/*.yaml-patch; do \
		[ -f "$$f" ] || continue; \
		target="$${f%-patch}"; \
		echo "Patching $$target"; \
		$(YAML_PATCH) -o "$$f" < "$$target" > "$$target.tmp" && mv "$$target.tmp" "$$target"; \
	done

## helm-deps: Update Helm chart dependencies
.PHONY: helm-deps
helm-deps:
	helm dependency update deploy/helm/kbind-provider-portal

# OCM / Helm publishing parameters
OCM ?= ocm
HELM ?= helm
OCM_REPO ?= ghcr.io/platform-mesh
OCM_CTF ?= .ocm/transport.ctf
# Component name (must match constructor/component-constructor.yaml).
OCM_COMPONENT ?= github.com/platform-mesh/kbind-provider
# Component constructor file. The local variant omits image resources (images are loaded
# into kind via `kind load` instead), avoiding the need for ghcr.io access during build.
#   make ocm-build                                    # release: resolves images from ghcr.io
OCM_CONSTRUCTOR ?= constructor/component-constructor.yaml
# Charts are published under this repo's own GHCR namespace (self-contained, alongside
# the container images) rather than the shared helm-charts registry.
HELM_REPO ?= ghcr.io/platform-mesh/kbind-provider/charts
VERSION ?= 0.0.0-dev
CHART_VERSION ?= $(VERSION)
IMAGE_VERSION ?= $(VERSION)
# OCI registry tag for the referenced local images (free-form, e.g. "latest" or "0.1.0").
# Defaults to "latest" so local builds resolve against an existing tag; CI sets the release tag.
OCI_TAG ?= latest
# Charts this repo owns. The OCM component embeds them and publishes them as OCI artifacts
# on `ocm-push`; `helm-push` is the standalone (non-OCM) publish path.
HELM_CHARTS ?= kbind-provider-operator kbind-provider-portal

# Helm chart that ships the operator and its CRDs.
OPERATOR_CHART ?= deploy/helm/kbind-provider-operator

## ocm-stamp-chart-versions: Stamp each chart's Chart.yaml version and appVersion
# Required before ocm-build: OCM's `input: type: helm` embeds charts from disk, so
# Chart.yaml must carry the correct version for Flux HelmRelease to accept the chart
# (it rejects charts where the OCI tag and Chart.yaml version differ). Also stamps
# appVersion so charts that fall back to .Chart.AppVersion for the image tag pull
# the correct release image instead of the hardcoded placeholder.
.PHONY: ocm-stamp-chart-versions
ocm-stamp-chart-versions:
	@for chart in $(HELM_CHARTS); do \
	  echo "==> stamping deploy/helm/$$chart/Chart.yaml version=$(CHART_VERSION) appVersion=$(IMAGE_VERSION)"; \
	  yq -i '.version = "$(CHART_VERSION)" | .appVersion = "$(IMAGE_VERSION)"' deploy/helm/$$chart/Chart.yaml || exit 1; \
	done

## ocm-build: Build OCM component archive (CTF) from constructor/component-constructor.yaml
# Charts are embedded directly from deploy/helm/ via `input: type: helm`.
# Run `make helm-deps` first to ensure chart dependencies are fetched.
.PHONY: ocm-build
ocm-build: ocm-stamp-chart-versions
	mkdir -p $(dir $(OCM_CTF))
	rm -rf $(OCM_CTF)
	$(OCM) add components -c --templater=go --file $(OCM_CTF) $(OCM_CONSTRUCTOR) -- \
	  VERSION=$(VERSION) \
	  CHART_VERSION=$(CHART_VERSION) \
	  IMAGE_VERSION=$(IMAGE_VERSION) \
	  OCI_TAG=$(OCI_TAG) \
	  IMAGE_REGISTRY=$(IMAGE_REGISTRY)

## ocm-push: Transfer the OCM component archive to $(OCM_REPO)
# --copy-resources / --copy-local-resources relocate the image OCI references into
# $(OCM_REPO), making the component fully self-contained.
.PHONY: ocm-push
ocm-push: ocm-build
	$(OCM) transfer ctf --overwrite --copy-resources --copy-local-resources $(OCM_CTF) $(OCM_REPO)

## ocm-release: Full release path — push images, publish the portal chart, then build + push the OCM component
# Prerequisites run in order: images-push → helm-push (portal chart as OCI) → ocm-push.
.PHONY: ocm-release
ocm-release: images-push helm-push ocm-push

## ocm-describe: Print the locally built component descriptor
.PHONY: ocm-describe
ocm-describe: ocm-build
	$(OCM) get componentversions --repo $(OCM_CTF) -o yaml

## ocm-inspect: List the resources of the locally built OCM component (name/type/relation/access)
.PHONY: ocm-inspect
ocm-inspect: ocm-build
	$(OCM) get resources --repo $(OCM_CTF) $(OCM_COMPONENT):$(VERSION) -o wide

## helm-push: Package and push deployable Helm charts to $(HELM_REPO) as OCI artifacts
.PHONY: helm-push
helm-push:
	mkdir -p $(BUILD_DIR)/charts
	@for chart in $(HELM_CHARTS); do \
	  echo "==> stamping deploy/helm/$$chart image.tag=$(OCI_TAG)"; \
	  yq -i 'with(select(.image != null); .image.tag = "$(OCI_TAG)")' deploy/helm/$$chart/values.yaml || exit 1; \
	  echo "==> packaging $$chart $(CHART_VERSION)"; \
	  $(HELM) dependency build deploy/helm/$$chart || exit 1; \
	  $(HELM) package deploy/helm/$$chart \
	    --version $(CHART_VERSION) \
	    --app-version $(IMAGE_VERSION) \
	    --destination $(BUILD_DIR)/charts || exit 1; \
	  echo "==> pushing $$chart-$(CHART_VERSION).tgz to oci://$(HELM_REPO)"; \
	  $(HELM) push $(BUILD_DIR)/charts/$$chart-$(CHART_VERSION).tgz oci://$(HELM_REPO) || exit 1; \
	done


## help: Display this help
.PHONY: help
help:
	@echo "Usage:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'
