FROM alpine:3.6

ENV GOPATH=/go KA_VER=v1.7.0

WORKDIR /go/src/github.com/utilitywarehouse/kube-applier
COPY . /go/src/github.com/utilitywarehouse/kube-applier
COPY templates/* /templates/
COPY static/ /static/
RUN apk --no-cache add curl ca-certificates git go musl-dev && \
 curl -O https://storage.googleapis.com/kubernetes-release/release/${KA_VER}/bin/linux/amd64/kubectl && \
 mv kubectl /usr/local/bin/kubectl && \
 chmod +x /usr/local/bin/kubectl && \
 go get ./... && \
 CGO_ENABLED=0 go build -ldflags '-s -extldflags "-static"' -o /kube-applier . && \
 apk del go musl-dev curl && \
 rm -rf $GOPATH /var/cache/apk/*

CMD [ "/kube-applier" ]
