FROM golang:alpine AS build
WORKDIR /go/src/github.com/utilitywarehouse/kube-applier
COPY . /go/src/github.com/utilitywarehouse/kube-applier
RUN apk --no-cache add git gcc musl-dev && \
 go get -t ./... && \
 go test ./... && \
 CGO_ENABLED=0 go build -o /kube-applier .

FROM alpine
ENV KUBECTL_VERSION v1.14.1
COPY templates/* /templates/
COPY static/ /static/
RUN apk --no-cache add git && \
 wget -O /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl && \
 chmod +x /usr/local/bin/kubectl
COPY --from=build /kube-applier /kube-applier
CMD [ "/kube-applier" ]
