// Package run implements structs for scheduling and performing apply runs that
// apply manifest files from a git repository source based on configuration
// stored in Application CRDs and scheduling.
package run

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/client"
	"github.com/utilitywarehouse/kube-applier/git"
	"github.com/utilitywarehouse/kube-applier/kubectl"
	"github.com/utilitywarehouse/kube-applier/log"
	"github.com/utilitywarehouse/kube-applier/metrics"
	"github.com/utilitywarehouse/kube-applier/sysutil"
)

const (
	defaultRunnerWorkerCount = 2
	defaultWorkerQueueSize   = 512
)

// Request defines an apply run request
type Request struct {
	Type        Type
	Application *kubeapplierv1alpha1.Application
}

// ApplyOptions contains global configuration for Apply
type ApplyOptions struct {
	ClusterResources    []string
	NamespacedResources []string
}

func (o *ApplyOptions) pruneWhitelist(app *kubeapplierv1alpha1.Application, pruneBlacklist []string) []string {
	var pruneWhitelist []string
	if app.Spec.Prune {
		pruneWhitelist = append(pruneWhitelist, o.NamespacedResources...)

		if app.Spec.PruneClusterResources {
			pruneWhitelist = append(pruneWhitelist, o.ClusterResources...)
		}

		// Trim blacklisted items out of the whitelist
		pruneBlacklist := uniqueStrings(append(pruneBlacklist, app.Spec.PruneBlacklist...))
		for _, b := range pruneBlacklist {
			for i, w := range pruneWhitelist {
				if b == w {
					pruneWhitelist = append(pruneWhitelist[:i], pruneWhitelist[i+1:]...)
					break
				}
			}
		}
	}
	return pruneWhitelist
}

func uniqueStrings(in []string) []string {
	m := make(map[string]bool)
	for _, i := range in {
		m[i] = true
	}
	out := make([]string, len(m))
	i := 0
	for v := range m {
		out[i] = v
		i++
	}
	return out
}

// Runner manages the full process of an apply run, including getting the
// appropriate files, running apply commands on them, and handling the results.
type Runner struct {
	Clock          sysutil.ClockInterface
	DiffURLFormat  string
	DryRun         bool
	KubeClient     *client.Client
	KubectlClient  *kubectl.Client
	PruneBlacklist []string
	RepoPath       string
	WorkerCount    int
	workerGroup    sync.WaitGroup
	workerQueue    chan Request
}

// Start runs a continuous loop that starts a new run when a request comes into the queue channel.
func (r *Runner) Start() chan<- Request {
	if r.workerQueue != nil {
		log.Logger.Info("Runner is already started, will not do anything")
		return nil
	}

	if r.WorkerCount == 0 {
		r.WorkerCount = defaultRunnerWorkerCount
	}
	r.workerQueue = make(chan Request, defaultWorkerQueueSize)
	r.workerGroup = sync.WaitGroup{}
	r.workerGroup.Add(r.WorkerCount)
	for i := 0; i < r.WorkerCount; i++ {
		go r.applyWorker()
	}
	return r.workerQueue
}

func (r *Runner) applyWorker() {
	defer r.workerGroup.Done()
	for request := range r.workerQueue {
		// TODO: for brevity, we could do:
		// app := request.Application
		appId := fmt.Sprintf("%s/%s", request.Application.Namespace, request.Application.Name)
		log.Logger.Info("Started apply run", "app", appId)
		metrics.UpdateRunRequest(request.Type.String(), request.Application, -1)

		clusterResources, namespacedResources, err := r.KubeClient.PrunableResourceGVKs()
		if err != nil {
			log.Logger.Error("Could not compute list of prunable resources", "app", appId, "error", err)
			r.setRequestFailure(request, err)
			continue
		}
		applyOptions := &ApplyOptions{
			ClusterResources:    clusterResources,
			NamespacedResources: namespacedResources,
		}
		gitUtil, cleanupTemp, err := r.copyRepository(request.Application)
		if err != nil {
			log.Logger.Error("Could not create a repository copy", "app", appId, "error", err)
			r.setRequestFailure(request, err)
			continue
		}
		hash, err := gitUtil.HeadHashForPaths(request.Application.Spec.RepositoryPath)
		if err != nil {
			log.Logger.Error("Could not determine HEAD hash", "app", appId, "error", err)
			r.setRequestFailure(request, err)
			cleanupTemp()
			continue
		}

		r.apply(gitUtil.RepoPath, request.Application, applyOptions)

		// TODO: move these in apply()
		request.Application.Status.LastRun.Commit = hash
		request.Application.Status.LastRun.Type = request.Type.String()

		if err := r.KubeClient.UpdateApplicationStatus(context.TODO(), request.Application); err != nil {
			log.Logger.Warn("Could not update Application run info", "app", appId, "error", err)
		}

		if request.Application.Status.LastRun.Success {
			log.Logger.Debug(fmt.Sprintf("Apply run output for %s:\n%s\n%s", appId, request.Application.Status.LastRun.Command, request.Application.Status.LastRun.Output))
		} else {
			log.Logger.Warn(fmt.Sprintf("Apply run for %s encountered errors:\n%s", request.Application.Status.LastRun.ErrorMessage))
		}

		metrics.UpdateFromLastRun(request.Application)

		log.Logger.Info("Finished apply run", "app", appId)
		cleanupTemp()
	}
}

// setRequestFailure is used to update the status of an Application when the
// request failed to setup and before attempting to apply.
// TODO: it might be preferrable to convert these to events
func (r *Runner) setRequestFailure(req Request, err error) {
	req.Application.Status.LastRun = &kubeapplierv1alpha1.ApplicationStatusRun{
		Command:      "", // These fields are not available since the request
		Commit:       "", // failed before even attempting an apply
		Output:       "",
		ErrorMessage: err.Error(),
		// We don't need to provide accurate timestamps here, the request failed
		// during setup, before attempting to apply
		Finished: metav1.NewTime(r.Clock.Now()),
		Started:  metav1.NewTime(r.Clock.Now()),
		Success:  false,
		Type:     req.Type.String(),
	}
}

// Stop gracefully shuts down the Runner.
func (r *Runner) Stop() {
	if r.workerQueue == nil {
		return
	}
	close(r.workerQueue)
	r.workerGroup.Wait()
}

func (r *Runner) copyRepository(app *kubeapplierv1alpha1.Application) (*git.Util, func(), error) {
	var env []string
	root, sub, err := (&git.Util{RepoPath: r.RepoPath}).SplitPath()
	if err != nil {
		return nil, nil, err
	}
	tmpDir, err := ioutil.TempDir("", fmt.Sprintf("run_%s_%s_%d", app.Namespace, app.Name, r.Clock.Now().Unix()))
	if err != nil {
		return nil, nil, err
	}
	cleanupDirs := []string{tmpDir}
	if app.Spec.StrongboxKeyringSecretRef != "" {
		sbHome, err := r.setupStrongboxKeyring(app)
		if err != nil {
			return nil, nil, err
		}
		env = []string{fmt.Sprintf("STRONGBOX_HOME=%s", sbHome)}
		cleanupDirs = append(cleanupDirs, sbHome)
	}
	cleanup := func() {
		for _, v := range cleanupDirs {
			os.RemoveAll(v)
		}
	}
	path := filepath.Join(sub, app.Spec.RepositoryPath)
	if err := git.CloneRepository(root, tmpDir, path, env); err != nil {
		cleanup()
		return nil, nil, err
	}
	return &git.Util{RepoPath: filepath.Join(tmpDir, sub)}, cleanup, nil
}

func (r *Runner) setupStrongboxKeyring(app *kubeapplierv1alpha1.Application) (string, error) {
	secret, err := r.KubeClient.GetSecret(context.TODO(), app.Namespace, app.Spec.StrongboxKeyringSecretRef)
	if err != nil {
		return "", err
	}
	data, ok := secret.Data[".strongbox_keyring"]
	if !ok {
		return "", fmt.Errorf("Secret %s/%s does not contain key '.strongbox_keyring'", secret.Namespace, secret.Name)
	}
	tmpDir, err := ioutil.TempDir("", fmt.Sprintf("run_%s_%s_%d_strongbox", app.Namespace, app.Name, r.Clock.Now().Unix()))
	if err != nil {
		return "", err
	}
	if err := ioutil.WriteFile(filepath.Join(tmpDir, ".strongbox_keyring"), data, 0400); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}
	return tmpDir, nil
}

// Apply takes a list of files and attempts an apply command on each.
func (r *Runner) apply(rootPath string, app *kubeapplierv1alpha1.Application, options *ApplyOptions) {
	start := r.Clock.Now()
	path := filepath.Join(rootPath, app.Spec.RepositoryPath)
	log.Logger.Info("Applying files", "path", path)

	dryRunStrategy := "none"
	if r.DryRun || app.Spec.DryRun {
		dryRunStrategy = "server"
	}

	cmd, output, err := r.KubectlClient.Apply(path, kubectl.ApplyFlags{
		Namespace:      app.Namespace,
		DryRunStrategy: dryRunStrategy,
		PruneWhitelist: options.pruneWhitelist(app, r.PruneBlacklist),
		ServerSide:     app.Spec.ServerSideApply,
	})
	finish := r.Clock.Now()

	app.Status.LastRun = &kubeapplierv1alpha1.ApplicationStatusRun{
		Command:      cmd,
		Output:       output,
		ErrorMessage: "",
		Finished:     metav1.NewTime(finish),
		Started:      metav1.NewTime(start),
	}
	if err != nil {
		app.Status.LastRun.ErrorMessage = err.Error()
	} else {
		app.Status.LastRun.Success = true
	}
}

// Enqueue attempts to add a run request to the queue, timing out after 5
// seconds.
func Enqueue(queue chan<- Request, t Type, app *kubeapplierv1alpha1.Application) {
	appId := fmt.Sprintf("%s/%s", t, app.Namespace, app.Name)
	select {
	case queue <- Request{Type: t, Application: app}:
		log.Logger.Debug("Run queued", "app", appId)
		metrics.UpdateRunRequest(t.String(), app, 1)
	case <-time.After(5 * time.Second):
		log.Logger.Error("Timed out trying to queue a run, run queue is full", "app", appId)
		metrics.AddRunRequestQueueFailure(t.String(), app)
	}
}
