#!/bin/bash

# A go unit test starts a docker container that has golang installed.
# Volume mounts the source of the repo. Sets up the GOPATH and runs go test
# in the project root

set -x

cwd=$(pwd)
gopath=/go
repodir=${gopath}/src/github.com/box/kube-applier

sudo docker run -v ${cwd}:${repodir} docker-registry-vip.dev.box.net/jenkins/box-sl6-build-golang bash -c "export GOPATH=${gopath}; cd ${repodir}; go test -v ./... "

