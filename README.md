# kube-applier

kube-applier is a service that enables continuous deployment of Kubernetes objects by applying declarative configuration files from a Git repository to a Kubernetes cluster. 

kube-applier runs as a Pod in your cluster and watches the [Git repo](#mounting-the-git-repository) to ensure that the cluster objects are up-to-date with their associated spec files (JSON or YAML) in the repo.

Whenever a new commit to the repo occurs, or at a [specified interval](#run-interval), kube-applier performs a "run", issuing [kubectl apply](https://kubernetes.io/docs/user-guide/kubectl/v1.6/#apply) commands with pruning at namespace level. The convention is that level 1 subdirs of REPO_PATH represent k8s namespaces: the name of the dir is the same as the namespace and the dir contains manifests for the given namespace.

kube-applier serves a [status page](#status-ui) and provides [metrics](#metrics) for monitoring.

## Requirements
* [Go (1.7+)](https://golang.org/dl/)
* [Docker (1.10+)](https://docs.docker.com/engine/getstarted/step_one/#step-1-get-docker)
* [Kubernetes cluster (1.2+)](http://kubernetes.io/docs/getting-started-guides/binary_release/)
    * The kubectl version specified in the Dockerfile must be either the same minor release as the cluster API server, or one release behind the server (e.g. client 1.3 and server 1.4 is fine, but client 1.4 and server 1.3 is not).
    * There are [many](https://github.com/kubernetes/kubernetes/issues/40119) [known](https://github.com/kubernetes/kubernetes/issues/29542) [issues](https://github.com/kubernetes/kubernetes/issues/39906) with using `kubectl apply` to apply ThirdPartyResource objects. These issues will be [fixed in Kubernetes 1.6](https://github.com/kubernetes/kubernetes/pull/40666).

## Setup

Download the source code and build the container image.
```
$ go get github.com/utilitywarehouse/kube-applier
$ cd $GOPATH/src/github.com/utilitywarehouse/kube-applier
```

You will need to push the image to a registry in order to reference it in a Kubernetes container spec.

## Usage

### Container Spec
We suggest running kube-applier as a Deployment (see [demo/](https://github.com/box/kube-applier/tree/master/demo) for example YAML files). We only support running one replica at a time at this point, so there may be a gap in application if the node serving the replica goes hard down until it is rescheduled onto another node.

***IMPORTANT:*** The Pod containing the kube-applier container must be spawned in a namespace that has write permissions on all namespaces in the API Server (e.g. kube-system).

### Environment Variables

**Required:**
* `REPO_PATH` - (string) Absolute path to the directory containing configuration files to be applied. It must be a Git repository or a path within one. Level 1 subdirs of this directory represent kubernetes namespaces.
* `LISTEN_PORT` - (int) Port for the container. This should be the same port specified in the container spec.

**Optional:**
* `SERVER` - (string) Address of the Kubernetes API server. By default, discovery of the API server is handled by kube-proxy. If kube-proxy is not set up, the API server address must be specified with this environment variable (which is then written into a [kubeconfig file](http://kubernetes.io/docs/user-guide/kubeconfig-file/) on the backend). Authentication to the API server is handled by service account tokens. See [Accessing the Cluster](http://kubernetes.io/docs/user-guide/accessing-the-cluster/#accessing-the-api-from-a-pod) for more info.
* `POLL_INTERVAL_SECONDS` - (int) Number of seconds to wait between each check for new commits to the repo (default is 5). Set to 0 to disable the wait period.
* <a name="run-interval"></a>`FULL_RUN_INTERVAL_SECONDS` - (int) Number of seconds between automatic full runs (default is 300, or 5 minutes). Set to 0 to disable the wait period.
* `DIFF_URL_FORMAT` - (string) If specified, allows the status page to display a link to the source code referencing the diff for a specific commit. `DIFF_URL_FORMAT` should be a URL for a hosted remote repo that supports linking to a commit hash. Replace the commit hash portion with "%s" so it can be filled in by kube-applier (e.g. `https://github.com/kubernetes/kubernetes/commit/%s`).
* `DRY_RUN` - (bool) If true, kubectl command will be run with --dry-run flag. This means live configuration of the cluster is not changed.
* `LABEL` - (string) K8s label that applier will use to filter resources. Add label with value 'false' on resource to filter out resource. Resourses with missing label are not filtered out. Resources which are filtered out are not managed by the kube-applier. Applies to following resources:
  * `core/v1/ConfigMap`
  * `core/v1/Pod`
  * `core/v1/Service`
  * `batch/v1/Job`
  * `extensions/v1beta1/DaemonSet`
  * `extensions/v1beta1/Deployment`
  * `extensions/v1beta1/Ingress`
  * `apps/v1beta1/StatefulSet`
  * `autoscaling/v1/HorizontalPodAutoscaler`
* `NAMESPACE_LABEL` - (string) K8s namespace label which enables/disables dry-run at namespace level. Add label with value 'true' on namespaces which should be disabled. Namespaces with missing label are enabled. Applies to following resources:
  * `core/v1/Namespace`

### Mounting the Git Repository

There are two ways to mount the Git repository into the kube-applier container.

**1. Git-sync sidecar container**

Git-sync keeps a local directory up to date with a remote repo. The local directory resides in a shared emptyDir volume that is mounted in both the git-sync and kube-applier containers.

Reference the [git-sync](https://github.com/kubernetes/git-sync) repo for setup and usage.

**2. Host-mounted volume**

Mount a Git repository from a host directory. This can be useful when you want kube-applier to apply changes to an object without checking the modified spec file into a remote repo.
```
"volumes": [
   {
      "hostPath": {
         "path": <path-to-host-directory>
      },
      "name": "repo-volume"
   }
   ...
]
```

**What happens if the contents of the local Git repo change in the middle of a kube-applier run?**

If there are changes to files in the `$REPO_PATH` directory during a kube-applier run, those changes may or may not be reflected in that run, depending on the timing of the changes. 

Given that the `$REPO_PATH` directory is a Git repo or located within one, it is likely that the majority of changes will be associated with a Git commit. Thus, a change in the middle of a run will likely update the HEAD commit hash, which will immediately trigger another run upon completion of the current run (regardless of whether or not any of the changes were effective in the current run). However, changes that are not associated with a new Git commit will not trigger a run.

**If I remove a configuration file, will kube-applier remove the associated Kubernetes object?**

No. If a file is removed from the `$REPO_PATH` directory, kube-applier will no longer apply the file, but kube-applier **WILL NOT** delete the cluster object(s) described by the file. These objects must be manually cleaned up using `kubectl delete`.

### "Force Run" Feature
In rare cases, you may wish to trigger a kube-applier run without checking in a commit or waiting for the next scheduled run (e.g. some of your files failed to apply because of some background condition in the cluster, and you have fixed it since the last run). This can be accomplished with the "Force Run" button on the status page, which starts a run immediately if no run is currently in progress, or queues a run to start upon completion of the current run. Only one run may sit in the queue at any given time.

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
* **file_apply_count** - A [Counter](https://godoc.org/github.com/prometheus/client_golang/prometheus#Counter) for each file that has had an apply attempt over the lifetime of the container, incremented with each apply attempt and tagged by the filepath and the result of the attempt.

The Prometheus [HTTP API](https://prometheus.io/docs/querying/api/) (also see the [Go library](https://github.com/prometheus/client_golang/tree/master/api/prometheus)) can be used for querying the metrics server.

## Development

All contributions are welcome to this project. Please review our [contributing guidelines](CONTRIBUTING.md).

Some suggestions for running kube-applier locally for development:

* To reach kube-applier's webserver from your browser, you can use an [apiserver proxy URL](https://kubernetes.io/docs/concepts/cluster-administration/access-cluster/#manually-constructing-apiserver-proxy-urls).
* Although git-sync is recommended for live environments, using a [host-mounted volume](#mounting-the-git-repository) can simplify basic local usage of kube-applier.

## Testing

See our [contributing guidelines](CONTRIBUTING.md#step-7-run-the-tests).

## Support

Need to contact us directly? Email oss@box.com and be sure to include the name of this project in the subject.

## Copyright and License

Copyright 2016 Box, Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
