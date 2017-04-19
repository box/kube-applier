FROM alpine:3.5

ENV GOPATH=/go

WORKDIR /go/src/github.com/utilitywarehouse/kube-applier
ADD . /go/src/github.com/utilitywarehouse/kube-applier
ADD templates/* /templates/
ADD static/ /static/
ADD https://storage.googleapis.com/kubernetes-release/release/v1.6.1/bin/linux/amd64/kubectl /usr/local/bin/kubectl

RUN apk --update --no-cache add ca-certificates git go musl-dev \
  && chmod +x /usr/local/bin/kubectl \
  && go get ./... \
  && CGO_ENABLED=0 go build -ldflags '-s -extldflags "-static"' -o /kube-applier . \
  && apk del go musl-dev \
  && rm -rf $GOPATH /var/cache/apk/*

CMD [ "/kube-applier" ]