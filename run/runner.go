// Package run implements structs for scheduling and performing apply runs that
// apply manifest files from a git repository source based on configuration
// stored in Waybill CRDs and scheduling.
package run

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

	hostFragment = `Host %s_github_com
    HostName github.com
    IdentitiesOnly yes
    IdentityFile %s
    User git
`

	secretAllowedNamespacesAnnotation = "kube-applier.io/allowed-namespaces"
)

var (
	reKeyName     = regexp.MustCompile(`#\skube-applier:\skey_(\w+)`)
	reRepoAddress = regexp.MustCompile(`(^\s*-\s*ssh:\/\/)(github)\.(com)(.*$)`)
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
	ClusterResources     []string
	NamespacedResources  []string
	EnvironmentVariables []string
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
	DryRun         bool
	KubeClient     *client.Client
	KubectlClient  *kubectl.Client
	PruneBlacklist []string
	RepoPath       string
	Repository     *git.Repository
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
	if r.WorkerCount <= 0 {
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
		if err := r.processRequest(request); err != nil {
			r.captureRequestFailure(request, err)
		}
	}
}

func (r *Runner) processRequest(request Request) error {
	wbId := fmt.Sprintf("%s/%s", request.Waybill.Namespace, request.Waybill.Name)
	log.Logger("runner").Info("Started apply run", "waybill", wbId)
	metrics.UpdateRunRequest(request.Type.String(), request.Waybill, -1)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(request.Waybill.Spec.RunTimeout)*time.Second)
	defer cancel()

	clusterResources, namespacedResources, err := r.KubeClient.PrunableResourceGVKs()
	if err != nil {
		return fmt.Errorf("could not compute list of prunable resources: %w", err)
	}
	applyOptions := &ApplyOptions{
		ClusterResources:    clusterResources,
		NamespacedResources: namespacedResources,
	}
	delegateToken, err := r.getDelegateToken(ctx, request.Waybill)
	if err != nil {
		return fmt.Errorf("failed fetching delegate token: %w", err)
	}

	tmpHomeDir, tmpRepoDir, cleanupTemp, err := r.setupTempDirs(request.Waybill)
	if err != nil {
		return fmt.Errorf("could not setup temporary directories: %w", err)
	}
	defer cleanupTemp()
	gitSSHCommand, err := r.setupGitSSH(ctx, request.Waybill, tmpHomeDir)
	if err != nil {
		return fmt.Errorf("failed setting up repository clone: %w", err)
	}
	applyOptions.EnvironmentVariables = append(applyOptions.EnvironmentVariables, gitSSHCommand)
	// Set HOME to tmpHomeDir, this means that SSH should not pick up any
	// local SSH keys and use them for cloning
	applyOptions.EnvironmentVariables = append(applyOptions.EnvironmentVariables, fmt.Sprintf("HOME=%s", tmpHomeDir))
	tmpRepoPath, hash, err := r.setupRepositoryClone(ctx, request.Waybill, tmpHomeDir, tmpRepoDir)
	if err != nil {
		return fmt.Errorf("failed setting up repository clone: %w", err)
	}

	r.apply(ctx, tmpRepoPath, delegateToken, request.Waybill, applyOptions)

	request.Waybill.Status.LastRun.Commit = hash
	request.Waybill.Status.LastRun.Type = request.Type.String()

	if err := r.KubeClient.UpdateWaybillStatus(ctx, request.Waybill); err != nil {
		log.Logger("runner").Warn("Could not update Waybill run info", "waybill", wbId, "error", err)
	}

	if request.Waybill.Status.LastRun.Success {
		log.Logger("runner").Debug(fmt.Sprintf("Apply run output for %s:\n%s\n%s", wbId, request.Waybill.Status.LastRun.Command, request.Waybill.Status.LastRun.Output))
	} else {
		log.Logger("runner").Warn(fmt.Sprintf("Apply run for %s encountered errors:\n%s", wbId, request.Waybill.Status.LastRun.ErrorMessage))
	}

	metrics.UpdateFromLastRun(request.Waybill)

	log.Logger("runner").Info("Finished apply run", "waybill", wbId)
	return nil
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

func (r *Runner) getDelegateToken(ctx context.Context, waybill *kubeapplierv1alpha1.Waybill) (string, error) {
	secret, err := r.KubeClient.GetSecret(ctx, waybill.Namespace, waybill.Spec.DelegateServiceAccountSecretRef)
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
	tmpHomeDir, err := os.MkdirTemp("", fmt.Sprintf("run_%s_%s_home_", waybill.Namespace, waybill.Name))
	if err != nil {
		return "", "", nil, err
	}
	tmpRepoDir, err := os.MkdirTemp("", fmt.Sprintf("run_%s_%s_repo_", waybill.Namespace, waybill.Name))
	if err != nil {
		os.RemoveAll(tmpHomeDir)
		return "", "", nil, err
	}
	return tmpHomeDir, tmpRepoDir, func() { os.RemoveAll(tmpHomeDir); os.RemoveAll(tmpRepoDir) }, nil
}

func (r *Runner) setupStrongboxKeyring(ctx context.Context, waybill *kubeapplierv1alpha1.Waybill, tmpHomeDir string) error {
	if waybill.Spec.StrongboxKeyringSecretRef == nil {
		return nil
	}
	sbNamespace := waybill.Spec.StrongboxKeyringSecretRef.Namespace
	if sbNamespace == "" {
		sbNamespace = waybill.Namespace
	}
	secret, err := r.KubeClient.GetSecret(ctx, sbNamespace, waybill.Spec.StrongboxKeyringSecretRef.Name)
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
	if err := os.WriteFile(filepath.Join(tmpHomeDir, ".strongbox_keyring"), strongboxData, 0400); err != nil {
		return err
	}
	return nil
}

func (r *Runner) setupRepositoryClone(ctx context.Context, waybill *kubeapplierv1alpha1.Waybill, tmpHomeDir, tmpRepoDir string) (string, string, error) {
	if err := r.setupStrongboxKeyring(ctx, waybill, tmpHomeDir); err != nil {
		return "", "", err
	}
	repositoryPath := waybill.Spec.RepositoryPath
	if repositoryPath == "" {
		repositoryPath = waybill.Namespace
	}
	subpath := filepath.Join(r.RepoPath, repositoryPath)
	hash, err := r.Repository.CloneLocal(ctx, []string{fmt.Sprintf("STRONGBOX_HOME=%s", tmpHomeDir)}, tmpRepoDir, subpath)
	if err != nil {
		return "", "", err
	}
	// Rewrite repo addresses for those that want to use SSH keys to clone
	if err := r.updateRepoBaseAddresses(tmpRepoDir); err != nil {
		return "", "", err
	}
	return filepath.Join(tmpRepoDir, r.RepoPath), hash, nil
}

// updateRepoBaseAddresses finds all Kustomization files by walking the repo dir.
// For each Kustomization file, we read it line by line trying to find KA key
// comment `# kube-applier: key_foobar`, we then attempt to replace the domain on
// the next line by injecting the `foobar` part into domain, resulting in
// `foobar_github_com`. We must not use `.` - as it breaks Host matching in
// .ssh/config
func (r *Runner) updateRepoBaseAddresses(tmpRepoDir string) error {
	kFiles := []string{}
	filepath.WalkDir(tmpRepoDir, func(path string, info fs.DirEntry, err error) error {
		if filepath.Base(path) == "kustomization.yaml" ||
			filepath.Base(path) == "kustomization.yml" ||
			filepath.Base(path) == "Kustomization" {
			kFiles = append(kFiles, path)
		}
		return nil
	})
	for _, k := range kFiles {
		var out []byte
		in, err := os.Open(k)
		if err != nil {
			return nil
		}
		defer in.Close()
		keyName := ""
		scanner := bufio.NewScanner(in)
		for scanner.Scan() {
			l := scanner.Bytes()
			if keyName == "" && reRepoAddress.Match(l) {
				log.Logger("runner").Warn("Detected ssh:// resource without a key annotation", "resource", string(l))
			}
			if keyName != "" {
				if reRepoAddress.Match(l) {
					l = reRepoAddress.ReplaceAll(l, []byte(fmt.Sprintf("${1}%s_${2}_${3}${4}", keyName)))
				}
				keyName = ""
			} else if reKeyName.Match(l) {
				s := reKeyName.FindSubmatch(l)
				if len(s) == 2 {
					keyName = string(s[1])
				}
			}
			out = append(out, l...)
			out = append(out, "\n"...)
		}
		if err := os.WriteFile(k, out, 0644); err != nil {
			return err
		}
	}
	return nil
}

// setupGitSSH ensures that any custom SSH keys configured for the Waybill are
// written to the temporary home directory and returns a value for GIT_SSH_COMMAND
// (man git) that forces Git (and therefore kustomize) to custom SSH command for
// cloning. Specifically, using a custom config file means we can match SSH keys
// to specific repositories (man ssh_config).
func (r *Runner) setupGitSSH(ctx context.Context, waybill *kubeapplierv1alpha1.Waybill, tmpHomeDir string) (string, error) {
	sshDir := filepath.Join(tmpHomeDir, ".ssh")
	os.Mkdir(sshDir, 0700)
	if waybill.Spec.GitSSHSecretRef == nil {
		// Even when there is no git SSH secret defined, we still override the
		// git ssh command (pointing the key to /dev/null) in order to avoid
		// using ssh keys in default system locations and to surface the error
		// if bases over ssh have been configured.
		return `GIT_SSH_COMMAND=ssh -q -F none -o IdentitiesOnly=yes -o IdentityFile=/dev/null -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no`, nil
	}
	gsNamespace := waybill.Spec.GitSSHSecretRef.Namespace
	if gsNamespace == "" {
		gsNamespace = waybill.Namespace
	}
	secret, err := r.KubeClient.GetSecret(ctx, gsNamespace, waybill.Spec.GitSSHSecretRef.Name)
	if err != nil {
		return "", err
	}
	if err := checkSecretIsAllowed(waybill, secret); err != nil {
		return "", err
	}
	knownHostsFragment := `-o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no`
	configFilename := filepath.Join(sshDir, "config")
	body, err := r.constructSSHConfig(secret, sshDir, configFilename)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(configFilename, body, 0644); err != nil {
		return "", err
	}
	for k, v := range secret.Data {
		if k == "known_hosts" {
			if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), v, 0600); err != nil {
				return "", err
			}
			knownHostsFragment = fmt.Sprintf(`-o UserKnownHostsFile=%[1]s/known_hosts`, sshDir)
		}
	}
	return fmt.Sprintf(`GIT_SSH_COMMAND=ssh -q -F %s %s`, configFilename, knownHostsFragment), nil
}

func (r *Runner) constructSSHConfig(secret *corev1.Secret, sshDir, configFilename string) ([]byte, error) {
	hostFragments := []string{}
	for k, v := range secret.Data {
		if strings.HasPrefix(k, "key_") {
			// if the file containing the SSH key does not have a newline at the end,
			// ssh does not complain about it but the key will not work properly
			if !bytes.HasSuffix(v, []byte("\n")) {
				v = append(v, byte('\n'))
			}
			keyFilename := filepath.Join(sshDir, fmt.Sprintf("%s", k))
			if err := os.WriteFile(keyFilename, v, 0600); err != nil {
				return []byte{}, err
			}
			nameFromKey := strings.TrimPrefix(k, "key_")
			hostFragments = append(hostFragments, fmt.Sprintf(hostFragment, nameFromKey, keyFilename))
		}
	}
	if len(hostFragments) == 0 {
		return nil, fmt.Errorf(`secret "%s/%s" does not contain any keys`, secret.Namespace, secret.Name)
	}
	return []byte(strings.Join(hostFragments, "\n")), nil
}

// Apply takes a list of files and attempts an apply command on each.
func (r *Runner) apply(ctx context.Context, rootPath, token string, waybill *kubeapplierv1alpha1.Waybill, options *ApplyOptions) {
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
		ctx,
		path,
		kubectl.ApplyOptions{
			Namespace:      waybill.Namespace,
			DryRunStrategy: dryRunStrategy,
			Environment:    options.EnvironmentVariables,
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
