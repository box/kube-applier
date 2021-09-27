FROM golang:1.17-alpine AS build

WORKDIR /src

RUN apk --no-cache add git gcc make musl-dev curl bash openssh-client

ENV \
  STRONGBOX_VERSION=0.2.0 \
  KUBECTL_VERSION=v1.21.0 \
  KUSTOMIZE_VERSION=v4.4.0

RUN os=$(go env GOOS) && arch=$(go env GOARCH) \
  && curl -Ls -o /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/${os}/${arch}/kubectl \
  && chmod +x /usr/local/bin/kubectl \
  && curl -Ls https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize/${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_${os}_${arch}.tar.gz \
    | tar xz -C /usr/local/bin/ \
  && chmod +x /usr/local/bin/kustomize \
  && curl -Ls -o /usr/local/bin/strongbox https://github.com/uw-labs/strongbox/releases/download/v${STRONGBOX_VERSION}/strongbox_${STRONGBOX_VERSION}_${os}_${arch} \
  && chmod +x /usr/local/bin/strongbox \
  && strongbox -git-config

COPY go.mod go.sum /src/
RUN go mod download

COPY . /src
RUN go get -t ./... \
  && make test \
  && CGO_ENABLED=0 && go build -o /kube-applier .

FROM alpine:3.14
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
