// Package run implements structs for scheduling and performing apply runs that
// apply manifest files from a git repository source based on configuration
// stored in Waybill CRDs and scheduling.
package run

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

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

	secretAllowedNamespacesAnnotation = "kube-applier.io/allowed-namespaces"
)

// Checks whether the provided Secret can be used by the Waybill and returns an
// error if it is not allowed.
func checkSecretIsAllowed(waybill *kubeapplierv1alpha1.Waybill, secret *corev1.Secret) error {
	if secret.Namespace == waybill.Namespace {
		return nil
	}
	allowedNamespaces := strings.Split(secret.Annotations[secretAllowedNamespacesAnnotation], ",")
	allowed := false
	for _, v := range allowedNamespaces {
		if strings.TrimSpace(v) == waybill.Namespace {
			allowed = true
			break
		}
	}
	if allowed {
		return nil
	}
	return fmt.Errorf(`secret "%s/%s" cannot be used in namespace "%s", the namespace must be listed in the '%s' annotation`, secret.Namespace, secret.Name, waybill.Namespace, secretAllowedNamespacesAnnotation)
}

// Request defines an apply run request
type Request struct {
	Type    Type
	Waybill *kubeapplierv1alpha1.Waybill
}

// ApplyOptions contains global configuration for Apply
type ApplyOptions struct {
	ClusterResources    []string
	NamespacedResources []string
}

func (o *ApplyOptions) pruneWhitelist(waybill *kubeapplierv1alpha1.Waybill, pruneBlacklist []string) []string {
	var pruneWhitelist []string
	if pointer.BoolPtrDerefOr(waybill.Spec.Prune, true) {
		pruneWhitelist = append(pruneWhitelist, o.NamespacedResources...)

		if waybill.Spec.PruneClusterResources {
			pruneWhitelist = append(pruneWhitelist, o.ClusterResources...)
		}

		// Trim blacklisted items out of the whitelist
		pruneBlacklist := uniqueStrings(append(pruneBlacklist, waybill.Spec.PruneBlacklist...))
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
	workerGroup    *sync.WaitGroup
	workerQueue    chan Request
}

// Start runs a continuous loop that starts a new run when a request comes into the queue channel.
func (r *Runner) Start() chan<- Request {
	if r.workerGroup != nil {
		log.Logger("runner").Info("Runner is already started, will not do anything")
		return nil
	}
	if r.WorkerCount == 0 {
		r.WorkerCount = defaultRunnerWorkerCount
	}
	r.workerQueue = make(chan Request, defaultWorkerQueueSize)
	r.workerGroup = &sync.WaitGroup{}
	r.workerGroup.Add(r.WorkerCount)
	for i := 0; i < r.WorkerCount; i++ {
		go r.applyWorker()
	}
	return r.workerQueue
}

func (r *Runner) applyWorker() {
	defer r.workerGroup.Done()
	for request := range r.workerQueue {
		wbId := fmt.Sprintf("%s/%s", request.Waybill.Namespace, request.Waybill.Name)
		log.Logger("runner").Info("Started apply run", "waybill", wbId)
		metrics.UpdateRunRequest(request.Type.String(), request.Waybill, -1)

		clusterResources, namespacedResources, err := r.KubeClient.PrunableResourceGVKs()
		if err != nil {
			r.captureRequestFailure(request, fmt.Errorf("could not compute list of prunable resources: %w", err))
			continue
		}
		applyOptions := &ApplyOptions{
			ClusterResources:    clusterResources,
			NamespacedResources: namespacedResources,
		}
		delegateToken, err := r.getDelegateToken(request.Waybill)
		if err != nil {
			r.captureRequestFailure(request, fmt.Errorf("failed fetching delegate token: %w", err))
			continue
		}

		tmpHomeDir, tmpRepoDir, cleanupTemp, err := r.setupTempDirs(request.Waybill)
		if err != nil {
			r.captureRequestFailure(request, fmt.Errorf("could not setup temporary directories: %w", err))
			continue
		}
		tmpRepoPath, repositoryPath, err := r.setupRepositoryClone(request.Waybill, tmpHomeDir, tmpRepoDir)
		if err != nil {
			r.captureRequestFailure(request, fmt.Errorf("failed setting up repository clone: %w", err))
			cleanupTemp()
			continue
		}

		hash, err := (&git.Util{RepoPath: tmpRepoPath}).HeadHashForPaths(repositoryPath)
		if err != nil {
			r.captureRequestFailure(request, fmt.Errorf("could not determine HEAD hash: %w", err))
			cleanupTemp()
			continue
		}

		r.apply(tmpRepoPath, delegateToken, request.Waybill, applyOptions)

		request.Waybill.Status.LastRun.Commit = hash
		request.Waybill.Status.LastRun.Type = request.Type.String()

		if err := r.KubeClient.UpdateWaybillStatus(context.TODO(), request.Waybill); err != nil {
			log.Logger("runner").Warn("Could not update Waybill run info", "waybill", wbId, "error", err)
		}

		if request.Waybill.Status.LastRun.Success {
			log.Logger("runner").Debug(fmt.Sprintf("Apply run output for %s:\n%s\n%s", wbId, request.Waybill.Status.LastRun.Command, request.Waybill.Status.LastRun.Output))
		} else {
			log.Logger("runner").Warn(fmt.Sprintf("Apply run for %s encountered errors:\n%s", wbId, request.Waybill.Status.LastRun.ErrorMessage))
		}

		metrics.UpdateFromLastRun(request.Waybill)

		log.Logger("runner").Info("Finished apply run", "waybill", wbId)
		cleanupTemp()
	}
}

// captureRequestFailure is used to capture a request failure that occured
// before attempting to apply. The reason is logged and emitted as a kubernetes
// event.
func (r *Runner) captureRequestFailure(req Request, err error) {
	wbId := fmt.Sprintf("%s/%s", req.Waybill.Namespace, req.Waybill.Name)
	log.Logger("runner").Error("Run request failed", "waybill", wbId, "error", err)
	r.KubeClient.EmitWaybillEvent(req.Waybill, corev1.EventTypeWarning, "WaybillRunRequestFailed", err.Error())
}

// Stop gracefully shuts down the Runner.
func (r *Runner) Stop() {
	if r.workerGroup == nil {
		return
	}
	close(r.workerQueue)
	r.workerGroup.Wait()
	r.workerGroup = nil
}

func (r *Runner) getDelegateToken(waybill *kubeapplierv1alpha1.Waybill) (string, error) {
	secret, err := r.KubeClient.GetSecret(context.TODO(), waybill.Namespace, waybill.Spec.DelegateServiceAccountSecretRef)
	if err != nil {
		return "", err
	}
	if secret.Type != corev1.SecretTypeServiceAccountToken {
		return "", fmt.Errorf(`secret "%s/%s" is not of type %s`, secret.Namespace, secret.Name, corev1.SecretTypeServiceAccountToken)
	}
	delegateToken, ok := secret.Data["token"]
	if !ok {
		return "", fmt.Errorf(`secret "%s/%s" does not contain key 'token'`, secret.Namespace, secret.Name)
	}
	return string(delegateToken), nil
}

func (r *Runner) setupTempDirs(waybill *kubeapplierv1alpha1.Waybill) (string, string, func(), error) {
	tmpHomeDir, err := ioutil.TempDir("", fmt.Sprintf("run_%s_%s_%d_home_", waybill.Namespace, waybill.Name, r.Clock.Now().Unix()))
	if err != nil {
		return "", "", nil, err
	}
	tmpRepoDir, err := ioutil.TempDir("", fmt.Sprintf("run_%s_%s_%d_repo_", waybill.Namespace, waybill.Name, r.Clock.Now().Unix()))
	if err != nil {
		os.RemoveAll(tmpHomeDir)
		return "", "", nil, err
	}
	return tmpHomeDir, tmpRepoDir, func() { os.RemoveAll(tmpHomeDir); os.RemoveAll(tmpRepoDir) }, nil
}

func (r *Runner) setupStrongboxKeyring(waybill *kubeapplierv1alpha1.Waybill, tmpHomeDir string) error {
	if waybill.Spec.StrongboxKeyringSecretRef == nil {
		return nil
	}
	sbNamespace := waybill.Spec.StrongboxKeyringSecretRef.Namespace
	if sbNamespace == "" {
		sbNamespace = waybill.Namespace
	}
	secret, err := r.KubeClient.GetSecret(context.TODO(), sbNamespace, waybill.Spec.StrongboxKeyringSecretRef.Name)
	if err != nil {
		return err
	}
	if err := checkSecretIsAllowed(waybill, secret); err != nil {
		return err
	}
	strongboxData, ok := secret.Data[".strongbox_keyring"]
	if !ok {
		return fmt.Errorf(`secret "%s/%s" does not contain key '.strongbox_keyring'`, secret.Namespace, secret.Name)
	}
	if err := ioutil.WriteFile(filepath.Join(tmpHomeDir, ".strongbox_keyring"), strongboxData, 0400); err != nil {
		return err
	}
	return nil
}

func (r *Runner) setupRepositoryClone(waybill *kubeapplierv1alpha1.Waybill, tmpHomeDir, tmpRepoDir string) (string, string, error) {
	if err := r.setupStrongboxKeyring(waybill, tmpHomeDir); err != nil {
		return "", "", err
	}
	root, sub, err := (&git.Util{RepoPath: r.RepoPath}).SplitPath()
	if err != nil {
		return "", "", err
	}
	repositoryPath := waybill.Spec.RepositoryPath
	if repositoryPath == "" {
		repositoryPath = waybill.Namespace
	}
	subpath := filepath.Join(sub, repositoryPath)
	if err := git.CloneRepository(root, tmpRepoDir, subpath, []string{fmt.Sprintf("STRONGBOX_HOME=%s", tmpHomeDir)}); err != nil {
		return "", "", err
	}
	return filepath.Join(tmpRepoDir, sub), repositoryPath, nil
}

// Apply takes a list of files and attempts an apply command on each.
func (r *Runner) apply(rootPath, token string, waybill *kubeapplierv1alpha1.Waybill, options *ApplyOptions) {
	start := r.Clock.Now()
	repositoryPath := waybill.Spec.RepositoryPath
	if repositoryPath == "" {
		repositoryPath = waybill.Namespace
	}
	path := filepath.Join(rootPath, repositoryPath)
	log.Logger("runner").Info("Applying files", "path", path)

	dryRunStrategy := "none"
	if r.DryRun || waybill.Spec.DryRun {
		dryRunStrategy = "server"
	}

	cmd, output, err := r.KubectlClient.Apply(
		path,
		kubectl.ApplyOptions{
			Namespace:      waybill.Namespace,
			DryRunStrategy: dryRunStrategy,
			PruneWhitelist: options.pruneWhitelist(waybill, r.PruneBlacklist),
			ServerSide:     waybill.Spec.ServerSideApply,
			Token:          token,
		},
	)
	finish := r.Clock.Now()

	waybill.Status.LastRun = &kubeapplierv1alpha1.WaybillStatusRun{
		Command:      cmd,
		Output:       output,
		ErrorMessage: "",
		Finished:     metav1.NewTime(finish),
		Started:      metav1.NewTime(start),
	}
	if err != nil {
		waybill.Status.LastRun.ErrorMessage = err.Error()
	} else {
		waybill.Status.LastRun.Success = true
	}
}

// Enqueue attempts to add a run request to the queue, timing out after 5
// seconds.
func Enqueue(queue chan<- Request, t Type, waybill *kubeapplierv1alpha1.Waybill) {
	wbId := fmt.Sprintf("%s/%s", waybill.Namespace, waybill.Name)
	if t != ForcedRun && !pointer.BoolPtrDerefOr(waybill.Spec.AutoApply, true) {
		log.Logger("runner").Debug("Run ignored, waybill autoApply is disabled", "waybill", wbId, "type", t)
		return
	}
	select {
	case queue <- Request{Type: t, Waybill: waybill}:
		log.Logger("runner").Debug("Run queued", "waybill", wbId, "type", t)
		metrics.UpdateRunRequest(t.String(), waybill, 1)
	case <-time.After(5 * time.Second):
		log.Logger("runner").Error("Timed out trying to queue a run, run queue is full", "waybill", wbId, "type", t)
		metrics.AddRunRequestQueueFailure(t.String(), waybill)
	}
}
