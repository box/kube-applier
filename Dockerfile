FROM golang:1.12-alpine AS build
WORKDIR /go/src/github.com/utilitywarehouse/kube-applier
COPY . /go/src/github.com/utilitywarehouse/kube-applier
ENV CGO_ENABLED 0
RUN apk --no-cache add git &&\
  go get -t ./... &&\
  go test ./... &&\
  go build -o /kube-applier .

FROM alpine:3.10
ENV KUBECTL_VERSION v1.14.2
COPY templates/ /templates/
COPY static/ /static/
RUN apk --no-cache add git openssh-client &&\
  git config --global url.ssh://git@github.com/.insteadOf https://github.com/ &&\
  wget -O /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl &&\
  chmod +x /usr/local/bin/kubectl
COPY --from=build /kube-applier /kube-applier

RUN echo "kube-applier:x:65533:65533::/tmp:/sbin/nologin" >> /etc/passwd

WORKDIR /tmp
USER kube-applier:nogroup

CMD [ "/kube-applier" ]
