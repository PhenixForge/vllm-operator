# Makefile for vllm-operator

BINARY_NAME := vllm-operator
IMAGE_NAME := ghcr.io/phenixforge/vllm-operator
IMAGE_TAG := latest
KUSTOMIZE := kustomize
CONTROLLER_GEN := controller-gen

GOPATH ?= $(shell go env GOPATH)
GOBIN := $(GOPATH)/bin
PATH += $(GOBIN)

.PHONY: all
all: build

.PHONY: build
build:
	go build -o bin/$(BINARY_NAME) -v ./...

.PHONY: manifests
manifests: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: controller-gen
controller-gen:
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..." output:crd:artifacts:config=config/crd/bases

.PHONY: install
install: manifests kustomize
	cd config/crd && $(KUSTOMIZE) edit set image controller=$(IMAGE_NAME):$(IMAGE_TAG)
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall:
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

.PHONY: run
run: manifests
	go run ./main.go

.PHONY: docker-build
docker-build:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

.PHONY: docker-push
docker-push:
	docker push $(IMAGE_NAME):$(IMAGE_TAG)

.PHONY: deploy
deploy: docker-build docker-push
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMAGE_NAME):$(IMAGE_TAG)
	$(KUSTOMIZE) build config/manager | kubectl apply -f -

.PHONY: test
test:
	go test ./... -coverprofile cover.out

.PHONY: clean
clean:
	rm -rf bin/
	rm -f cover.out

.PHONY: boilerplate
boilerplate:
	mkdir -p hack
	cat > hack/boilerplate.go.txt <<EOF
// Copyright 2026 PhenixForge
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
EOF

.PHONY: kubebuilder
kubebuilder:
	curl -L -o kubebuilder https://go.kubebuilder.io/dl/latest/$(go env GOOS)/$(go env GOARCH)
	chmod +x kubebuilder
	mv kubebuilder /usr/local/bin/

.PHONY: controller-gen-install
controller-gen-install:
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest

.PHONY: kustomize
kustomize:
	go install sigs.k8s.io/kustomize/kustomize/v5@latest

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  all           - Build the project"
	@echo "  build         - Build the operator binary"
	@echo "  manifests     - Generate CRD manifests and deepcopy code"
	@echo "  install       - Install CRDs into the cluster"
	@echo "  uninstall     - Uninstall CRDs from the cluster"
	@echo "  run           - Run the operator locally"
	@echo "  docker-build  - Build the Docker image"
	@echo "  docker-push   - Push the Docker image"
	@echo "  deploy        - Deploy the operator to the cluster"
	@echo "  test          - Run tests"
	@echo "  clean         - Clean up build artifacts"