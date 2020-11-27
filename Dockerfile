FROM golang:1.15-alpine AS build

WORKDIR /src

RUN apk --no-cache add git gcc musl-dev curl

ENV \
  KUBEBUILDER_VERSION=2.3.1 \
  STRONGBOX_VERSION=0.2.0 \
  KUBECTL_VERSION=v1.19.2 \
  KUSTOMIZE_VERSION=v3.8.5
RUN os=$(go env GOOS) && arch=$(go env GOARCH) &&\
  curl -Ls -o /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/${os}/${arch}/kubectl &&\
  chmod +x /usr/local/bin/kubectl &&\
  curl -Ls https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize/${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_${os}_${arch}.tar.gz |\
  tar xz -C /usr/local/bin/ &&\
  chmod +x /usr/local/bin/kustomize &&\
  curl -Ls -o /usr/local/bin/strongbox https://github.com/uw-labs/strongbox/releases/download/v${STRONGBOX_VERSION}/strongbox_${STRONGBOX_VERSION}_${os}_${arch} &&\
  chmod +x /usr/local/bin/strongbox &&\
  strongbox -git-config &&\
  curl -Ls https://go.kubebuilder.io/dl/${KUBEBUILDER_VERSION}/${os}/${arch} |\
  tar -xz -C /tmp/ &&\
  mv /tmp/kubebuilder_${KUBEBUILDER_VERSION}_${os}_${arch} /usr/local/kubebuilder
ENV PATH=$PATH:/usr/local/kubebuilder/bin

COPY go.mod go.sum /src/
RUN go mod download

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
