FROM alpine:3.7

ENV GOPATH=/go

WORKDIR /go/src/github.com/utilitywarehouse/kube-applier
COPY . /go/src/github.com/utilitywarehouse/kube-applier
COPY templates/* /templates/
COPY static/ /static/

RUN \
 apk --no-cache add curl ca-certificates git go musl-dev && \
 curl -sSL -o /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/v1.10.4/bin/linux/amd64/kubectl && \
 curl -sSL -o /usr/local/bin/kustomize https://github.com/kubernetes-sigs/kustomize/releases/download/v1.0.3/kustomize_1.0.3_linux_amd64 && \
 chmod +x /usr/local/bin/kubectl && \
 chmod +x /usr/local/bin/kustomize && \
 go get -t ./... && \
 go test ./... && \
 CGO_ENABLED=0 go build -ldflags '-s -extldflags "-static"' -o /kube-applier . && \
 apk del curl go musl-dev && rm -r /go

CMD [ "/kube-applier" ]
