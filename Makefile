SHELL := /bin/bash

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

.PHONY: manifests generate controller-gen test build run release

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) \
		crd:trivialVersions=true \
		paths="./..." \
		output:crd:artifacts:config=manifests/base/cluster
	@{ \
	cd manifests/base/cluster ;\
	kustomize edit add resource kube-applier.io_* ;\
	}

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test:
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/$(shell grep controller-runtime go.mod | cut -d' ' -f2)/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); CGO_ENABLED=1 && go test -v -race -count=1 -cover ./...

build:
	docker build -t kube-applier .

run:
	docker run \
	-e DIFF_URL_FORMAT=$${DIFF_URL_FORMAT} \
	-e DRY_RUN=$${DRY_RUN} \
	-e GIT_POLL_WAIT=$${GIT_POLL_WAIT} \
	-e GIT_KNOWN_HOSTS_PATH=$${GIT_KNOWN_HOSTS_PATH} \
	-e GIT_SSH_KEY_PATH=$${GIT_SSH_KEY_PATH} \
	-e LISTEN_PORT=$${LISTEN_PORT} \
	-e LOG_LEVEL=$${LOG_LEVEL} \
	-e OIDC_CALLBACK_URL=$${OIDC_CALLBACK_URL} \
	-e OIDC_CLIENT_ID=$${OIDC_CLIENT_ID} \
	-e OIDC_CLIENT_SECRET=$${OIDC_CLIENT_SECRET} \
	-e OIDC_ISSUER=$${OIDC_ISSUER} \
	-e PRUNE_BLACKLIST=$${PRUNE_BLACKLIST} \
	-e REPO_BRANCH=$${REPO_BRANCH} \
	-e REPO_DEPTH=$${REPO_DEPTH} \
	-e REPO_DEST=$${REPO_DEST} \
	-e REPO_PATH=$${REPO_PATH} \
	-e REPO_REMOTE=$${REPO_REMOTE} \
	-e REPO_REVISION=$${REPO_REVISION} \
	-e REPO_SYNC_INTERVAL=$${REPO_SYNC_INTERVAL} \
	-e REPO_TIMEOUT=$${REPO_TIMEOUT} \
	-e STATUS_UPDATE_INTERVAL=$${STATUS_UPDATE_INTERVAL} \
	-e WAYBILL_POLL_INTERVAL=$${WAYBILL_POLL_INTERVAL} \
	-e WORKER_COUNT=$${WORKER_COUNT} \
	-v $${HOME}/.kube:/root/.kube \
	-p 8080:8080 \
	-ti kube-applier

# Hack to take arguments from command line
# Usage: `make release 5.5.5`
# https://stackoverflow.com/questions/6273608/how-to-pass-argument-to-makefile-from-command-line
release:
	sed -i 's#utilitywarehouse/kube-applier:.*#utilitywarehouse/kube-applier:$(filter-out $@,$(MAKECMDGOALS))#g' manifests/base/server/kube-applier.yaml
	sed -i -E 's#kube-applier//manifests/base/(client|cluster|server)\?ref=.*#kube-applier//manifests/base/\1?ref=$(filter-out $@,$(MAKECMDGOALS))#g' README.md manifests/example/kustomization.yaml

%:		# matches any task name
	@:	# empty recipe = do nothing
