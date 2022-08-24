FROM golang:1.7 as STAGE_BUILD

WORKDIR $GOPATH/src/github.com/box/kube-applier

COPY . $GOPATH/src/github.com/box/kube-applier

RUN make build

FROM ubuntu

WORKDIR /root/

ADD templates/* /templates/
ADD static/ /static/

RUN apt-get update && \
    apt-get install -y git kubectl

RUN chmod +x /usr/local/bin/kubectl

COPY --from=STAGE_BUILD /go/src/github.com/box/kube-applier/kube-applier /kube-applier
