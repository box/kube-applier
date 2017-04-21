FROM alpine:3.5

ADD templates/* /templates/

ADD static/ /static/

ADD build.sh /
RUN /build.sh

COPY kube-applier /kube-applier
