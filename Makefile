image_repo=registry.hub.docker.com/steamforvietnam/kube-applier
image_tag=v0.3.0
image=${image_repo}:${image_tag}

all: build

ENVVAR = GOOS=linux GOARCH=amd64 CGO_ENABLED=0
TAG = v0.2.0
GODEP_BIN = $$GOPATH/bin/godep

deps:
	go get github.com/tools/godep

build: clean deps fmt
	$(ENVVAR) $(GODEP_BIN) go build -o kube-applier

container:
	docker build \
		-t ${image} \
		.

push:
	docker push ${image}
.PHONY: push

clean:
	rm -f kube-applier

fmt:
	find . -path ./vendor -prune -o -name '*.go' -print | xargs -L 1 -I % gofmt -s -w %

test-unit: clean deps fmt build
	$(GODEP_BIN) go test -v --race ./...

.PHONY: all deps build container clean fmt test-unit
