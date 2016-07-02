# Copyright 2016 The Kubernetes Authors All rights reserved.
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

# Use the native vendor/ dependency system
export GO15VENDOREXPERIMENT=1

# Bump this on release
VERSION ?= v0.4.0

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
BUILD_DIR ?= ./out
REPOPATH ?= k8s.io/minikube
BUILD_IMAGE ?= gcr.io/google_containers/kube-cross:v1.6.2-1

ifeq ($(IN_DOCKER),1)
    GOPATH := /go
else
    # Not defined locally GOPATH since shell will not have the value set
    # see http://savannah.gnu.org/bugs/?10593
    # If defined locally, we then need to set the env var for each call to go
    EXPECTED_GOPATH := $(shell pwd)/_gopath
    ifndef GOPATH
        $(error GOPATH is not set. Must be set to $(EXPECTED_GOPATH))
    endif
    ifneq ($(GOPATH), $(EXPECTED_GOPATH))
        $(error GOPATH is expexted to be $(EXPECTED_GOPATH))
    endif
endif


# Use system python if it exists, otherwise use Docker.
PYTHON := $(shell command -v python || echo "docker run --rm -it -v $(shell pwd):/minikube -w /minikube python python")
BUILD_OS := $(shell uname -s)

# Set the version information for the Kubernetes servers, and build localkube statically
K8S_VERSION_LDFLAGS := $(shell $(PYTHON) hack/get_k8s_version.py 2>&1)
MINIKUBE_LDFLAGS := -X k8s.io/minikube/pkg/version.version=$(VERSION)
LOCALKUBE_LDFLAGS := "$(K8S_VERSION_LDFLAGS) $(MINIKUBE_LDFLAGS) -s -w -extldflags '-static'"

clean:
	rm -rf $(GOPATH)
	rm -rf $(BUILD_DIR)
	rm -f pkg/minikube/cluster/localkubecontents.go

ifeq ($(IN_DOCKER),1)
	# Windows Virtual box does not support symlinks
    MKGOPATH := if [ ! -d $(GOPATH)/src/$(REPOPATH) ];  then exit 1 ; fi
else
    MKGOPATH := mkdir -p $(shell dirname $(GOPATH)/src/$(REPOPATH)) && ln -s -f $(shell pwd) $(GOPATH)/src/$(REPOPATH)
endif


LOCALKUBEFILES := $(shell go list  -f '{{join .Deps "\n"}}' ./cmd/localkube/ | grep k8s.io | xargs go list -f '{{ range $$file := .GoFiles }} {{$$.Dir}}/{{$$file}}{{"\n"}}{{end}}')
MINIKUBEFILES := $(shell go list  -f '{{join .Deps "\n"}}' ./cmd/minikube/ | grep k8s.io | xargs go list -f '{{ range $$file := .GoFiles }} {{$$.Dir}}/{{$$file}}{{"\n"}}{{end}}')

OUTPUT := $(BUILD_DIR)/minikube
ifeq ($(GOOS),windows)
	OUTPUT := $(OUTPUT).exe
endif
out/minikube: out/minikube-$(GOOS)-$(GOARCH)
	cp $(BUILD_DIR)/minikube-$(GOOS)-$(GOARCH) $(OUTPUT)

out/localkube: $(LOCALKUBEFILES)
	$(MKGOPATH)
ifeq ($(BUILD_OS),Linux)
	CGO_ENABLED=1 go build -ldflags=$(LOCALKUBE_LDFLAGS) -o $(BUILD_DIR)/localkube ./cmd/localkube
else
    # double the first slash to avoid POSIX-to-Windows path conversion in msys
	docker run -w //go/src/$(REPOPATH) -e IN_DOCKER=1 -v /$(shell pwd)://go/src/$(REPOPATH) $(BUILD_IMAGE) make out/localkube
endif

OUTPUT_OS_ARCH := $(BUILD_DIR)/minikube-$(GOOS)-$(GOARCH)
ifeq ($(GOOS),windows)
	OUTPUT_OS_ARCH := $(OUTPUT_OS_ARCH).exe
endif

out/minikube-$(GOOS)-$(GOARCH): $(MINIKUBEFILES) pkg/minikube/cluster/assets.go
	$(MKGOPATH)
	CGO_ENABLED=0 GOARCH=$(GOARCH) GOOS=$(GOOS) go build --installsuffix cgo -ldflags="$(MINIKUBE_LDFLAGS)" -a -o $(BUILD_DIR)/minikube-$(GOOS)-$(GOARCH) ./cmd/minikube

localkube-image: out/localkube
	make -C deploy/docker VERSION=$(VERSION)

iso:
	cd deploy/iso && ./build.sh

.PHONY: integration
integration: out/minikube
	go test -v $(REPOPATH)/test/integration --tags=integration

.PHONY: test
test: pkg/minikube/cluster/assets.go
	$(MKGOPATH)
	./test.sh

pkg/minikube/cluster/assets.go: out/localkube $(GOPATH)/bin/go-bindata deploy/iso/addon-manager.yaml deploy/addons/dashboard-rc.yaml deploy/addons/dashboard-svc.yaml
	$(GOPATH)/bin/go-bindata -nomemcopy -o pkg/minikube/cluster/assets.go -pkg cluster ./out/localkube deploy/iso/addon-manager.yaml deploy/addons/dashboard-rc.yaml deploy/addons/dashboard-svc.yaml

$(GOPATH)/bin/go-bindata:
	$(MKGOPATH)
	go get github.com/jteeuwen/go-bindata/...
