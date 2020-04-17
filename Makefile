SHELL := /bin/bash

.PHONY: generate-mocks build run

generate-mocks:
	mockgen -package=git -source=git/gitutil.go -destination=git/mock_gitutil.go
	mockgen -package=sysutil -source sysutil/clock.go -destination=sysutil/mock_clock.go 
	mockgen -package=metrics -source metrics/prometheus.go -destination=metrics/mock_prometheus.go
	mockgen -package=kube -source kube/client.go -destination=kube/mock_client.go
	mockgen -package=kubectl -source kubectl/client.go -destination=kubectl/mock_client.go

build:
	docker build -t kube-applier .

run:
	docker run \
	-e REPO_PATH=/src/manifests/$${CLUSTER_DIR} \
	-e REPO_PATH_FILTERS=$${REPO_PATH_FILTERS} \
	-e DIFF_URL_FORMAT=$${DIFF_URL_FORMAT} \
	-e LISTEN_PORT=$${LISTEN_PORT} \
	-e POLL_INTERVAL_SECONDS=$${POLL_INTERVAL_SECONDS} \
	-e FULL_RUN_INTERVAL_SECONDS=$${FULL_RUN_INTERVAL_SECONDS} \
	-e DRY_RUN=$${DRY_RUN} \
	-e LOG_LEVEL=$${LOG_LEVEL} \
	-v $${HOME}/.kube:/root/.kube \
	-v $${LOCAL_REPO_PATH}:/src/manifests:ro \
	-p 8080:8080 \
	-ti kube-applier

# Hack to take arguments from command line
# Usage: `make release 5.5.5`
# https://stackoverflow.com/questions/6273608/how-to-pass-argument-to-makefile-from-command-line
release:
	sed -i 's#utilitywarehouse/kube-applier:.*#utilitywarehouse/kube-applier:$(filter-out $@,$(MAKECMDGOALS))#g' manifests/base/kube-applier.yaml
	sed -i 's#kube-applier//manifests/base?ref=.*#kube-applier//manifests/base?ref=$(filter-out $@,$(MAKECMDGOALS))#g' README.md manifests/example/kustomization.yaml

%:		# matches any task name
	@:	# empty recipe = do nothing
