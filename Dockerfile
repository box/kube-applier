FROM golang:1.16 as builder
WORKDIR $GOPATH/src/github.com/box/kube-applier
COPY . $GOPATH/src/github.com/box/kube-applier
RUN make build

FROM ubuntu
LABEL maintainer="exklamationmark<mark@bitsgofer.com>"
WORKDIR /root/
ADD templates/* /templates/
ADD static/ /static/
RUN apt-get update && \
    apt-get install -y git
ADD https://storage.googleapis.com/kubernetes-release/release/v1.21.3/bin/linux/amd64/kubectl /usr/local/bin/kubectl
RUN chmod +x /usr/local/bin/kubectl
COPY --from=builder /go/src/github.com/box/kube-applier/kube-applier /kube-applier
