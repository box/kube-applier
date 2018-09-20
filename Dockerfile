FROM golang:alpine AS build
WORKDIR /go/src/app
COPY . /go/src/app/
RUN apk --no-cache add git gcc musl-dev && \
 go get -t ./... && \
 go test ./... && \
 CGO_ENABLED=0 go build -o /kube-applier .

FROM alpine:3.8
COPY templates/* /templates/
COPY static/ /static/
RUN apk --no-cache add git && \
 wget -O /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/v1.11.0/bin/linux/amd64/kubectl && \
 wget -O /usr/local/bin/kustomize https://github.com/kubernetes-sigs/kustomize/releases/download/v1.0.8/kustomize_1.0.8_linux_amd64 && \
 chmod +x /usr/local/bin/kubectl && \
 chmod +x /usr/local/bin/kustomize
COPY --from=build /kube-applier /kube-applier
CMD [ "/kube-applier" ]
