TAG=0.5.0-kubectlv1.7.0

build:
	docker build -t utilitywarehouse/kube-applier .

tag:
	docker tag utilitywarehouse/kube-applier utilitywarehouse/kube-applier:$(TAG)

push:
	docker push utilitywarehouse/kube-applier:$(TAG)
