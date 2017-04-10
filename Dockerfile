FROM ubuntu

ADD templates/* /templates/

ADD static/ /static/

RUN apt-get update && \
    apt-get install -y git

ADD https://storage.googleapis.com/kubernetes-release/release/v1.5.4/bin/linux/amd64/kubectl /usr/local/bin/kubectl

RUN chmod +x /usr/local/bin/kubectl

COPY kube-applier /kube-applier
