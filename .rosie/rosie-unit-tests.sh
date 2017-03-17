#!/bin/bash

# A go unit test starts a docker container that has golang installed.
# Volume mounts the source of the repo. Sets up the GOPATH and runs go test
# in the project root

cwd=$(pwd)
godir=/go/src/github.com/box

sudo docker run -it docker-registry-vip.dev.box.net/jenkins/box-sl6 \
    -v ${cwd}:${godir} bash -c \"ls\"







