# kube-applier

[![Project Status](http://opensource.box.com/badges/active.svg)](http://opensource.box.com/badges)

kube-applier is a service that enables continuous deployment of Kubernetes objects by applying declarative configuration files from a Git repository to a Kubernetes cluster. 

kube-applier runs as a pod in your cluster and watches a [Git repo](#mounting-the-git-repository) to ensure that the Kubernetes objects in the cluster are up-to-date with their associated spec files (JSON or YAML) in the repo.

Whenever a new commit to the repo occurs, or after a [certain length of time since the last commit](#run-interval), kube-applier performs a full run, applying all JSON and YAML files within the repo.

kube-applier serves a [status page](#status-ui) and provides [metrics](#metrics) for monitoring.

## Requirements
* [Go (1.7+)](https://golang.org/dl/)
* [Docker (1.10+)](https://docs.docker.com/engine/getstarted/step_one/#step-1-get-docker)
* [Kubernetes cluster (1.2+)](http://kubernetes.io/docs/getting-started-guides/binary_release/)
    * The kubectl version specified in the Dockerfile must be either the same minor release as the cluster API server, or one release behind the server (e.g. client 1.3 and server 1.4 is fine, but client 1.4 and server 1.3 is not).
    * There are [many](https://github.com/kubernetes/kubernetes/issues/40119) [known](https://github.com/kubernetes/kubernetes/issues/29542) [issues](https://github.com/kubernetes/kubernetes/issues/39906) with using `kubectl apply` to apply ThirdPartyResource objects. These issues will be [fixed in Kubernetes 1.6](https://github.com/kubernetes/kubernetes/pull/40666).

## Setup

Clone the repository into your GOPATH and build the container image.
```
$ git clone https://github.com/box/kube-applier $GOPATH/src/github.com/box/kube-applier
$ cd $GOPATH/src/github.com/box/kube-applier
$ make container
```

You will need to push the image to a registry in order to reference it in a Kubernetes container spec.

## Usage

### Container Spec
We suggest running kube-applier as a Deployment in the Kubernetes cluster that you want to apply objects to (see demo/ for example YAML files). We only support running one replica at a time at this point, so there may be a gap in application if the node serving the replica goes hard down until it's rescheduled onto another node.

***IMPORTANT:*** The Pod containing the kube-applier container must be spawned in a namespace that has write permissions on all namespaces in the API Server (e.g. kube-system).

### Environment Variables

**Required:**
* `REPO_PATH` - Absolute path to the root directory for the configuration files to be applied. It must be a Git repository or a path within one. All .json and .yaml files within this directory (and its subdirectories) will be applied, unless listed on the blacklist.
* `LISTEN_PORT` - Port for the container. This should be the same port specified in the container spec.

**Optional:**
* `SERVER` - Address of the Kubernetes API server. By default, discovery of the API server is handled by kube-proxy. If kube-proxy is not set up, the API server address must be specified with this environment variable (which is then written into a [kubeconfig file](http://kubernetes.io/docs/user-guide/kubeconfig-file/) on the backend). Authentication to the API server is handled by service account tokens. See [Accessing the Cluster](http://kubernetes.io/docs/user-guide/accessing-the-cluster/#accessing-the-api-from-a-pod) for more info.
* `BLACKLIST_PATH` - Path to a "blacklist" file which specifies files that should not be applied. This path should be absolute (e.g. `/k8s/conf/kube_applier_blacklist`), not relative to `REPO_PATH` (although you may want to check the blacklist file into the repo). The blacklist file itself should be a plaintext file, with a file path on each line. Each of these paths should be relative to `REPO_PATH` (for example, if `REPO_PATH` is set to `/git/repo`, and the file to be blacklisted is `/git/repo/apps/app1.json`, the line in the blacklist file should be `apps/app1.json`).
* `POLL_INTERVAL_SECONDS` - Number of seconds to wait between each check for new commits to the repo (default is 5). Set to 0 to disable the wait period.
* <a name="run-interval"></a>`FULL_RUN_INTERVAL_SECONDS` - Number of seconds between automatic full runs (default is 300, or 5 minutes). Set to 0 to disable the wait period.
* `DIFF_URL_FORMAT` - If specified, allows the status page to display a link to the source code referencing the diff for a specific commit. `DIFF_URL_FORMAT` should be a URL for a hosted remote repo that supports linking to a commit hash. Replace the commit hash portion with "%s" so it can be filled in by kube-applier (e.g. `https://github.com/kubernetes/kubernetes/commit/%s`).

### Mounting the Git Repository

There are two ways to mount the Git repository into the kube-applier container.

**1. Git-sync sidecar container**

Git-sync keeps a local directory up to date with a remote repo. The local directory resides in a shared emptyDir volume that is mounted in both the git-sync and kube-applier containers.

Reference the [git-sync](https://https://github.com/kubernetes/git-sync) repo for setup and usage.

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

A kube-applier run consists of two primary steps:

1. Walk the `$REPO_PATH` directory and build a list of files that should be applied.
2. Call a sequence of "kubectl apply" commands, one for each of the above files. 

If a file is added or removed during a run, the apply list in Step 1 will reflect the change if and only if the change occurs before the directory walk reaches that file's location.

If a file's contents are modified during a run, the change will be effective in the current run if and only if it occurs before the individual apply command is called for that specific file.

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

The HTML template for the status page lives in `templates/status.html`, and `static/` holds additional assets. The data is provided to the template as a `Result` struct from the `run` package (see `run/` and `webserver/`).

### Metrics
kube-applier uses [Prometheus](https://github.com/prometheus/client_golang) for metrics. Metrics are hosted/served at on the webserver at /metrics. In addition to the Prometheus default metrics, the following custom metrics are included:
* **run_latency_seconds** - A [Summary](https://godoc.org/github.com/prometheus/client_golang/prometheus#Summary) that keeps track of the durations of each apply run, tagged with a boolean for whether or not the run was a success (i.e. no failed apply attempts).
* **file_apply_count** - A [Counter](https://godoc.org/github.com/prometheus/client_golang/prometheus#Counter) for each file that has had an apply attempt over the lifetime of the container, incremented with each apply attempt and tagged by the filepath and the result of the attempt.

The Prometheus [HTTP API](https://prometheus.io/docs/querying/api/) (also see the [Go library](https://github.com/prometheus/client_golang/tree/master/api/prometheus)) can be used for querying the metrics server.

##Testing
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
