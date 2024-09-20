all: build

ENVVAR = GOOS=linux GOARCH=amd64 CGO_ENABLED=0
TAG = v0.2.0

deps:
	go mod tidy

build: clean deps fmt
	go build -o kube-applier

container:
	docker build -t kube-applier:$(TAG) .

clean:
	rm -f kube-applier

fmt:
	find . -path ./vendor -prune -o -name '*.go' -print | xargs -L 1 -I % gofmt -s -w %

test-unit: clean deps fmt build
	go test -v --race ./...

.PHONY: all deps build container clean fmt test-unit
