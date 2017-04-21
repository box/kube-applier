#!/bin/sh

set -ex
KUBECTL_VERSION=1.6.2
KUBECTL_HASH="9beec3e8a9208da5cac479a164a61bf6a7b0b8716c338f866c4316680f0e9d98"

get_bin()
{
  hash="$1"
  url="$2"
  outputdir="${3:-/usr/local/bin}"
  f=$(basename "$url")

  curl -sSL "$url" -o ${outputdir}/"$f"
  echo "$hash  ${outputdir}/${f}" | sha256sum -c - || exit 10
  chmod +x ${outputdir}/"$f"
}
apk add --update git curl ca-certificates

get_bin $KUBECTL_HASH \
    "https://storage.googleapis.com/kubernetes-release/release/v${KUBECTL_VERSION}/bin/linux/amd64/kubectl"

# Cleanup
rm -rf /tmp/* /var/tmp/* /var/cache/apk/* /var/cache/distfiles/*
