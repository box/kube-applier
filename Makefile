all: build

ENVVAR = GOOS=linux GOARCH=amd64 CGO_ENABLED=0
TAG = v0.2.0
GODEP_BIN = $$GOPATH/bin/godep

deps:
	go get github.com/tools/godep

build: clean deps fmt
	$(ENVVAR) $(GODEP_BIN) go build -o kube-applier

DOCKER_NAMESPACE=cocktavern
DOCKER_CONTAINER_NAME=kube-applier
DOCKER_REPOSITORY=$(DOCKER_NAMESPACE)/$(DOCKER_CONTAINER_NAME)
DOCKER_PLATFORMS=linux/arm64

local-docker-build:
	docker build --rm --tag $(DOCKER_REPOSITORY):local .

ci-docker-auth:
	@echo "${DOCKER_PASSWORD}" | docker login --username "${DOCKER_USERNAME}" --password-stdin

ci-docker-build:
	@docker buildx build \
		--platform $(DOCKER_PLATFORMS) \
		--tag $(DOCKER_REPOSITORY):$(GITHUB_SHA) \
		--tag $(DOCKER_REPOSITORY):latest \
		--output "type=image,push=false" .

ci-docker-build-push: ci-docker-build
	@docker buildx build \
		--platform $(DOCKER_PLATFORMS) \
		--tag $(DOCKER_REPOSITORY):$(GITHUB_SHA) \
		--tag $(DOCKER_REPOSITORY):latest \
		--output "type=image,push=true" .

clean:
	rm -f kube-applier

fmt:
	find . -path ./vendor -prune -o -name '*.go' -print | xargs -L 1 -I % gofmt -s -w %

test-unit: clean deps fmt build
	$(GODEP_BIN) go test -v --race ./...

.PHONY: all deps build container clean fmt test-unit
