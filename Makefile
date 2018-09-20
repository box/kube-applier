generate-mocks:
	mockgen -package=git -source=git/gitutil.go -destination=git/mock_gitutil.go
	mockgen -package=sysutil -source sysutil/clock.go -destination=sysutil/mock_clock.go 
	mockgen -package=metrics -source metrics/prometheus.go -destination=metrics/mock_prometheus.go
	mockgen -package=kube -source kube/client.go -destination=kube/mock_client.go
