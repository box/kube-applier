FROM golang:1.13 as builder
WORKDIR $GOPATH/src/github.com/box/kube-applier
COPY . $GOPATH/src/github.com/box/kube-applier
RUN make build

FROM ubuntu
ARG KUBE_VERSION=v1.13.7
LABEL maintainer="Greg Lyons<glyons@box.com>"
WORKDIR /root/
ADD https://storage.googleapis.com/kubernetes-release/release/${KUBE_VERSION}/bin/linux/amd64/kubectl /usr/local/bin/kubectl
RUN chmod +x /usr/local/bin/kubectl
RUN apt-get update && \
    apt-get install -y git && \
    rm -rf /var/lib/apt/lists/*
ADD templates/* /templates/
ADD static/ /static/
COPY --from=builder /go/src/github.com/box/kube-applier/kube-applier /kube-applier
