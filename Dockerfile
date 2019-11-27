FROM golang:1.12-alpine AS build
WORKDIR /go/src/github.com/utilitywarehouse/kube-applier
COPY . /go/src/github.com/utilitywarehouse/kube-applier
ENV CGO_ENABLED 0
RUN apk --no-cache add git &&\
  go get -t ./... &&\
  go test ./... &&\
  go build -o /kube-applier .

FROM alpine:3.10
ENV KUBECTL_VERSION v1.16.2
ENV KUSTOMIZE_VERSION v3.4.0
COPY templates/ /templates/
COPY static/ /static/
RUN apk --no-cache add git openssh-client tini curl &&\
  wget -O /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl &&\
  chmod +x /usr/local/bin/kubectl &&\
  curl -s https://api.github.com/repos/kubernetes-sigs/kustomize/releases |\
    grep browser_download |\
    grep linux |\
    cut -d '"' -f 4 |\
    grep /kustomize/${KUSTOMIZE_VERSION} |\
    sort | tail -n 1 |\
    xargs curl -O -L &&\
  tar xzf ./kustomize_${KUSTOMIZE_VERSION}_linux_amd64.tar.gz -C /usr/local/bin &&\
  rm -f ./kustomize_${KUSTOMIZE_VERSION}_linux_amd64.tar.gz &&\
  chmod +x /usr/local/bin/kustomize
COPY --from=build /kube-applier /kube-applier

ENTRYPOINT ["/sbin/tini", "--"]
CMD [ "/kube-applier" ]
