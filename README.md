# kube-applier

[![Docker Repository on Quay](https://quay.io/repository/utilitywarehouse/kube-applier/status "Docker Repository on Quay")](https://quay.io/repository/utilitywarehouse/kube-applier)

Forked from: https://github.com/box/kube-applier

kube-applier is a service that enables continuous deployment of Kubernetes objects by applying declarative configuration files from a Git repository to a Kubernetes cluster. 

kube-applier runs as a Pod in your cluster and watches the [Git repo](#mounting-the-git-repository) to ensure that the cluster objects are up-to-date with their associated spec files (JSON or YAML) in the repo.

Whenever a new commit to the repo occurs, or at a [specified interval](#run-interval), kube-applier performs a "run", issuing [kubectl apply](https://kubernetes.io/docs/user-guide/kubectl/v1.6/#apply) commands with pruning at namespace level. The convention is that level 1 subdirs of REPO_PATH represent k8s namespaces: the name of the dir is the same as the namespace and the dir contains manifests for the given namespace.

kube-applier serves a [status page](#status-ui) and provides [metrics](#metrics) for monitoring.

## Requirements

* The kubectl version specified in the Dockerfile must be either the same minor release as the cluster API server, or one release behind the server (e.g. client 1.3 and server 1.4 is fine, but client 1.4 and server 1.3 is not).

## Usage

### Environment Variables

**Required:**
* `REPO_PATH` - (string) Absolute path to the directory containing configuration files to be applied. It must be a Git repository or a path within one. Level 1 subdirs of this directory represent kubernetes namespaces.
* `LISTEN_PORT` - (int) Port for the container. This should be the same port specified in the container spec.

**Optional:**
* `REPO_PATH_FILTERS` - (string) A comma separated list of sub directories to be applied. Supports [shell file name patterns](https://golang.org/pkg/path/filepath/#Match).
* `SERVER` - (string) Address of the Kubernetes API server. By default, discovery of the API server is handled by kube-proxy. If kube-proxy is not set up, the API server address must be specified with this environment variable (which is then written into a [kubeconfig file](http://kubernetes.io/docs/user-guide/kubeconfig-file/) on the backend). Authentication to the API server is handled by service account tokens. See [Accessing the Cluster](http://kubernetes.io/docs/user-guide/accessing-the-cluster/#accessing-the-api-from-a-pod) for more info.
* `POLL_INTERVAL_SECONDS` - (int) Number of seconds to wait between each check for new commits to the repo (default is 5).
* <a name="run-interval"></a>`FULL_RUN_INTERVAL_SECONDS` - (int) Number of seconds between automatic full runs (default is 300, or 5 minutes). Set to 0 to disable.
* `DIFF_URL_FORMAT` - (string) If specified, allows the status page to display a link to the source code referencing the diff for a specific commit. `DIFF_URL_FORMAT` should be a URL for a hosted remote repo that supports linking to a commit hash. Replace the commit hash portion with "%s" so it can be filled in by kube-applier (e.g. `https://github.com/kubernetes/kubernetes/commit/%s`).
* `DRY_RUN` - (bool) If true, kubectl command will be run with --server-dry-run flag. This means live configuration of the cluster is not changed.
* `DELEGATE_SERVICE_ACCOUNTS` - (bool) If true kube-applier will try to fetch a SA under each namespace and use it to run kubectl commands. It will error for all namespaces that do not contain a SA matching the value of `DELEGATE_SERVICE_ACCOUNT_NAME`. Defaults to `false`.
* `DELEGATE_SERVICE_ACCOUNT_NAME` - (string) The name of the service account used when `DELEGATE_SERVICE_ACCOUNTS` is `true`. Defaults to `kube-applier`.
* `LABEL` - (string) (on|dry-run|off)  K8s label which enables/disables automatic deployment. Label can either be specified at namespace level or on individual resources. Add label with value 'on' or 'dry-run' on a namespace to enable the namespace. By default namespaces are disabled. Add label with value 'off' on individual resources to disable the resource. Resources are enabled by default if their namespace is enabled. Only enabled resources are managed by the kube-applier. Applies to following resources:

	"apps/v1/DaemonSet",
	"apps/v1/Deployment",
	"apps/v1/StatefulSet",
	"autoscaling/v1/HorizontalPodAutoscaler",
	"batch/v1/Job",
	"core/v1/ConfigMap",
	"core/v1/Pod",
	"core/v1/Service",
	"core/v1/ServiceAccount",
	"networking.k8s.io/v1beta1/Ingress",
	"networking.k8s.io/v1/NetworkPolicy",

### Mounting the Git Repository

Git-sync keeps a local directory up to date with a remote repo. The local directory resides in a shared emptyDir volume that is mounted in both the git-sync and kube-applier containers.

Reference the [git-sync](https://github.com/kubernetes/git-sync) repo for setup and usage.

**What happens if the contents of the local Git repo change in the middle of a kube-applier run?**

If there are changes to files in the `$REPO_PATH` directory during a kube-applier run, those changes may or may not be reflected in that run, depending on the timing of the changes. 

Given that the `$REPO_PATH` directory is a Git repo or located within one, it is likely that the majority of changes will be associated with a Git commit. Thus, a change in the middle of a run will likely update the HEAD commit hash, which will immediately trigger another run upon completion of the current run (regardless of whether or not any of the changes were effective in the current run). However, changes that are not associated with a new Git commit will not trigger a run.

**If I remove a configuration file, will kube-applier remove the associated Kubernetes object?**

No. If a file is removed from the `$REPO_PATH` directory, kube-applier will no longer apply the file, but kube-applier **WILL NOT** delete the cluster object(s) described by the file. These objects must be manually cleaned up using `kubectl delete`.

## Deploying

Included is a Kustomize (https://kustomize.io/) base you can reference in your namespace:

```
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
bases:
- github.com/utilitywarehouse/kube-applier//manifests/kube-applier/base?ref=2.2.7
```

and patch as per example: [manifests/kube-applier/example/](manifests/kube-applier/example/)

There is also a base for creating the appropriate roles and service accounts in your managed namespaces (required when
`$DELEGATE_SERVICE_ACCOUNTS` is `true`):
```
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
bases:
- github.com/utilitywarehouse/kube-applier//manifests/rbac/base?ref=2.2.7
```

and an example patch: [manifests/rbac/example/](manifests/auth/example/)

## Monitoring
### Status UI
![screenshot](https://github.com/box/kube-applier/raw/master/static/img/status_page_screenshot.png "Status Page Screenshot")

kube-applier hosts a status page on a webserver, served at the service endpoint URL. The status page displays information about the most recent apply run, including:
* Start and end times
* Latency
* Most recent commit
* Blacklisted files
* Errors
* Files applied successfully

The HTML template for the status page lives in `templates/status.html`, and `static/` holds additional assets.

### Metrics
kube-applier uses [Prometheus](https://github.com/prometheus/client_golang) for metrics. Metrics are hosted on the webserver at /metrics (status UI is the index page). In addition to the Prometheus default metrics, the following custom metrics are included:
* **run_latency_seconds** - A [Summary](https://godoc.org/github.com/prometheus/client_golang/prometheus#Summary) that keeps track of the durations of each apply run, tagged with a boolean for whether or not the run was a success (i.e. no failed apply attempts).
* **namespace_apply_count** - A [Counter](https://godoc.org/github.com/prometheus/client_golang/prometheus#Counter) for each namespace that has had an apply attempt over the lifetime of the container, incremented with each apply attempt and tagged by the namespace and the result of the attempt.
* **result_summary** - A [Gauge](https://godoc.org/github.com/prometheus/client_golang/prometheus#Gauge) for each deployment, labelled with the namespace, action, status and type of object applied
* **kubectl_exit_code_count** - A [Counter](https://godoc.org/github.com/prometheus/client_golang/prometheus#Counter) for each exit code returned by executions of `kubectl`, labelled with the namespace and exit code.

The Prometheus [HTTP API](https://prometheus.io/docs/querying/api/) (also see the [Go library](https://github.com/prometheus/client_golang/tree/master/api/prometheus)) can be used for querying the metrics server.

## Copyright and License

Copyright 2016 Box, Inc. All rights reserved.

Copyright (c) 2017-2019 Utility Warehouse Ltd.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
