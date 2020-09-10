FROM golang:1.15-alpine AS build
WORKDIR /go/src/github.com/utilitywarehouse/kube-applier
COPY . /go/src/github.com/utilitywarehouse/kube-applier
ENV CGO_ENABLED 0
RUN apk --no-cache add git &&\
  go get -t ./... &&\
  go test ./... &&\
  go build -o /kube-applier .

FROM alpine:3.11
ENV KUBECTL_VERSION v1.18.8
ENV KUSTOMIZE_VERSION v3.8.2
COPY templates/ /templates/
COPY static/ /static/
RUN apk --no-cache add git openssh-client tini &&\
  wget -O /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl &&\
  chmod +x /usr/local/bin/kubectl &&\
  wget -O - https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2F${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_linux_amd64.tar.gz |\
  tar xz -C /usr/local/bin/
COPY --from=build /kube-applier /kube-applier

ENTRYPOINT ["/sbin/tini", "--"]
CMD [ "/kube-applier" ]
