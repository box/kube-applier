all: build

ENVVAR = GOOS=linux GOARCH=amd64 CGO_ENABLED=0
TAG = v0.1.0

deps:
	go get github.com/tools/godep

build: clean deps
	$(ENVVAR) godep go build -o kube-applier

container: build
	docker build -t kube-applier:$(TAG) .

clean:
	rm -f kube-applier

.PHONY: all deps build container clean
