FROM golang:1.15-alpine AS build

WORKDIR /src

RUN apk --no-cache add git gcc musl-dev curl bash

ENV \
  STRONGBOX_VERSION=0.2.0 \
  KUBECTL_VERSION=v1.20.2 \
  KUSTOMIZE_VERSION=v3.8.5
RUN os=$(go env GOOS) && arch=$(go env GOARCH) &&\
  curl -Ls -o /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/${os}/${arch}/kubectl &&\
  chmod +x /usr/local/bin/kubectl &&\
  curl -Ls https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize/${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_${os}_${arch}.tar.gz |\
  tar xz -C /usr/local/bin/ &&\
  chmod +x /usr/local/bin/kustomize &&\
  curl -Ls -o /usr/local/bin/strongbox https://github.com/uw-labs/strongbox/releases/download/v${STRONGBOX_VERSION}/strongbox_${STRONGBOX_VERSION}_${os}_${arch} &&\
  chmod +x /usr/local/bin/strongbox &&\
  strongbox -git-config

COPY go.mod go.sum /src/
RUN go mod download

ENV \
  ENVTEST_ASSETS_DIR=/usr/local/envtest \
  KUBEBUILDER_ASSETS=/usr/local/envtest/bin
RUN /bin/bash -c '\
  mkdir -p ${ENVTEST_ASSETS_DIR} &&\
  controller_runtime_version=$(grep controller-runtime go.mod | cut -d" " -f2) &&\
  curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/${controller_runtime_version}/hack/setup-envtest.sh &&\
  source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh &&\
  fetch_envtest_tools ${ENVTEST_ASSETS_DIR}'

COPY . /src
RUN go get -t ./... &&\
  CGO_ENABLED=1 && go test -race -count=1 ./... &&\
  CGO_ENABLED=0 && go build -o /kube-applier -ldflags '-s -w -extldflags "-static"' .

FROM alpine:3.12
RUN apk --no-cache add git openssh-client tini
COPY templates/ /templates/
COPY static/ /static/
COPY --from=build \
  /usr/local/bin/kubectl \
  /usr/local/bin/kustomize \
  /usr/local/bin/strongbox \
  /usr/local/bin/
COPY --from=build /root/.gitconfig /root/.gitconfig
COPY --from=build /kube-applier /kube-applier
ENTRYPOINT ["/sbin/tini", "--"]
CMD [ "/kube-applier" ]
