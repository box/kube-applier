FROM golang:alpine AS build
WORKDIR /go/src/github.com/utilitywarehouse/kube-applier
COPY . /go/src/github.com/utilitywarehouse/kube-applier
RUN apk --no-cache add git gcc musl-dev && \
 go get -t ./... && \
 go test ./... && \
 CGO_ENABLED=0 go build -o /kube-applier .

FROM alpine
ENV KUBECTL_VERSION v1.12.3
ENV KUSTOMIZE_VERSION 2.0.1
COPY templates/* /templates/
COPY static/ /static/
RUN apk --no-cache add git && \
 wget -O /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl && \
 wget -O /usr/local/bin/kustomize https://github.com/kubernetes-sigs/kustomize/releases/download/v${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_linux_amd64 && \
 chmod +x /usr/local/bin/kubectl && \
 chmod +x /usr/local/bin/kustomize
COPY --from=build /kube-applier /kube-applier
CMD [ "/kube-applier" ]
