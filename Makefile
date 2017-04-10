all: build

ENVVAR = GOOS=linux GOARCH=amd64 CGO_ENABLED=0
TAG = v0.1.0
GODEP_BIN = $$GOPATH/bin/godep

deps:
	go get github.com/tools/godep

build: clean deps
	$(ENVVAR) $(GODEP_BIN) go build -o kube-applier

container: build
	docker build -t kube-applier:$(TAG) .

clean:
	rm -f kube-applier

test-unit: clean deps build
	$(GODEP_BIN) go test -v --race ./...

.PHONY: all deps build container clean test-unit
