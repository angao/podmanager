# Image URL to use all building/pushing image targets
IMG ?= podmanager:1.0

all: manager

# Run tests
test: generate fmt
	go test ./pkg/... ./cmd/... -coverprofile cover.out

# Build manager binary
manager: generate fmt
	go build -o bin/manager pcc/podmanager/cmd/manager

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt
	go run ./cmd/manager/main.go

# Install CRDs into a cluster
install:
	kubectl apply -f config/crds

# Deploy controller in the configured Kubernetes cluster
deploy:
	kubectl apply -f config/rbac
	kubectl apply -f config/manager

# Generate manifests e.g. CRD, RBAC etc.
manifests:
	go run vendor/sigs.k8s.io/controller-tools/cmd/controller-gen/main.go all

# Run go fmt against code
fmt:
	go fmt ./pkg/... ./cmd/...

# Generate code
generate:
	go generate ./pkg/... ./cmd/...

# Build the docker image
docker-build:
	docker build . -t ${IMG}

# Push the docker image
docker-push:
	docker push ${IMG}
