// Package client defines a custom kubernetes Client for use with kube-applier
// and its custom resources.
package client

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	// +kubebuilder:scaffold:imports
	// For local dev
	//_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

const (
	// Name identifies this client and is used for ownership-related fields.
	Name = "kube-applier"
)

var (
	scheme = runtime.NewScheme()

	defaultUpdateOptions = &client.UpdateOptions{FieldManager: "kube-applier"}
)

func init() {
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		log.Fatalf("Cannot setup client scheme: %v", err)
	}

	if err := kubeapplierv1alpha1.AddToScheme(scheme); err != nil {
		log.Fatalf("Cannot setup client scheme: %v", err)
	}
	// +kubebuilder:scaffold:scheme
}

// Client encapsulates a kubernetes client for interacting with the apiserver.
type Client struct {
	cluster.Cluster
	clientset kubernetes.Interface
	shutdown  func()
}

// New returns a new kubernetes client.
func New(opts ...cluster.Option) (*Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("Cannot get kubernetes config: %v", err)
	}
	return newClient(cfg, opts...)
}

// NewWithConfig returns a new kubernetes client initialised with the provided
// configuration.
func NewWithConfig(cfg *rest.Config, opts ...cluster.Option) (*Client, error) {
	return newClient(cfg, opts...)
}

func newClient(cfg *rest.Config, opts ...cluster.Option) (*Client, error) {
	c, err := cluster.New(cfg, func(options *cluster.Options) {
		options.Scheme = scheme
		for _, opt := range opts {
			opt(options)
		}
	})
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	ctx, shutdown := context.WithCancel(context.Background())
	go c.Start(ctx)

	return &Client{
		Cluster:   c,
		clientset: clientset,
		shutdown:  shutdown,
	}, nil
}

// Shutdown shuts down the client
func (c *Client) Shutdown() {
	c.shutdown()
}

// CloneConfig copies the client's config into a new rest.Config, it does not
// copy user credentials
func (c *Client) CloneConfig() *rest.Config {
	return rest.AnonymousClientConfig(c.GetConfig())
}

// EmitWaybillEvent creates an Event for the provided Waybill.
func (c *Client) EmitWaybillEvent(waybill *kubeapplierv1alpha1.Waybill, eventType, reason, messageFmt string, args ...interface{}) {
	c.GetEventRecorderFor(Name).Eventf(waybill, eventType, reason, messageFmt, args...)
}

// HasAccess returns a boolean depending on whether the email address provided
// corresponds to a user who has edit access to the specified Waybill.
func (c *Client) HasAccess(ctx context.Context, waybill *kubeapplierv1alpha1.Waybill, email, verb string) (bool, error) {
	gvk := waybill.GroupVersionKind()
	plural, err := c.pluralName(gvk)
	if err != nil {
		return false, err
	}
	response, err := c.clientset.AuthorizationV1().SubjectAccessReviews().Create(
		ctx,
		&authorizationv1.SubjectAccessReview{
			Spec: authorizationv1.SubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: waybill.Namespace,
					Verb:      verb,
					Group:     gvk.Group,
					Version:   gvk.Version,
					Resource:  plural,
				},
				User: email,
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		return false, err
	}
	return response.Status.Allowed, nil
}

// ListWaybills returns a list of all the Waybill resources.
func (c *Client) ListWaybills(ctx context.Context) ([]kubeapplierv1alpha1.Waybill, error) {
	waybills := &kubeapplierv1alpha1.WaybillList{}
	if err := c.GetClient().List(ctx, waybills); err != nil {
		return nil, err
	}
	// ensure that the list of Waybills is sorted alphabetically
	sortedWaybills := make([]kubeapplierv1alpha1.Waybill, len(waybills.Items))
	for i, wb := range waybills.Items {
		sortedWaybills[i] = wb
	}
	sort.Slice(sortedWaybills, func(i, j int) bool {
		return sortedWaybills[i].Namespace+sortedWaybills[i].Name < sortedWaybills[j].Namespace+sortedWaybills[j].Name
	})

	unique := map[string]kubeapplierv1alpha1.Waybill{}
	for _, wb := range sortedWaybills {
		if v, ok := unique[wb.Namespace]; ok {
			c.EmitWaybillEvent(&wb, corev1.EventTypeWarning, "MultipleWaybillsFound", "Waybill %s is already being used", v.Name)
			continue
		}
		unique[wb.Namespace] = wb
	}
	ret := make([]kubeapplierv1alpha1.Waybill, len(unique))
	i := 0
	for _, wb := range unique {
		ret[i] = wb
		i++
	}
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].Namespace < ret[j].Namespace
	})
	return ret, nil
}

// GetWaybill returns the Waybill resource specified by the namespace
// and name.
func (c *Client) GetWaybill(ctx context.Context, namespace, name string) (*kubeapplierv1alpha1.Waybill, error) {
	waybill := &kubeapplierv1alpha1.Waybill{}
	if err := c.GetClient().Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, waybill); err != nil {
		return nil, err
	}
	return waybill, nil
}

// UpdateWaybill updates the Waybill resource provided.
func (c *Client) UpdateWaybill(ctx context.Context, waybill *kubeapplierv1alpha1.Waybill) error {
	return c.GetClient().Update(ctx, waybill, defaultUpdateOptions)
}

// UpdateWaybillStatus updates the status of the Waybill resource
// provided.
func (c *Client) UpdateWaybillStatus(ctx context.Context, waybill *kubeapplierv1alpha1.Waybill) error {
	return c.GetClient().Status().Update(ctx, waybill, defaultUpdateOptions)
}

// GetSecret returns the Secret resource specified by the namespace and name.
func (c *Client) GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	// Use the APIReader to bypass the cache and get secrets directly from
	// the API server.
	//
	// If it used the cache then it would cache ALL secrets,
	// which is a bit of an overreach given that we're only interested in
	// specific secrets.
	if err := c.GetAPIReader().Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// PrunableResourceGVKs returns the cluster and namespaced resources that the
// client can prune as two slices of strings of the format
// <group>/<version>/<kind>.
func (c *Client) PrunableResourceGVKs(ctx context.Context, namespace string) ([]string, []string, error) {
	var cluster, namespaced []string

	_, resourceList, err := c.clientset.Discovery().ServerGroupsAndResources()
	if err != nil {
		return cluster, namespaced, err
	}

	srr := &authorizationv1.SelfSubjectRulesReview{
		Spec: authorizationv1.SelfSubjectRulesReviewSpec{
			Namespace: namespace,
		},
	}
	reviewResp, err := c.clientset.AuthorizationV1().SelfSubjectRulesReviews().Create(ctx, srr, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		return cluster, namespaced, err
	}
	for _, l := range resourceList {
		groupVersion := l.GroupVersion
		if groupVersion == "v1" {
			groupVersion = "core/v1"
		}
		gv, err := schema.ParseGroupVersion(l.GroupVersion)
		if err != nil {
			return cluster, namespaced, err
		}

		for _, r := range l.APIResources {
			gvk := groupVersion + "/" + r.Kind
			if prunable(r) && rulesAllowPrune(reviewResp.Status.ResourceRules, gv.Group, r.Name) {
				if r.Namespaced {
					namespaced = append(namespaced, gvk)
				} else {
					cluster = append(cluster, gvk)
				}
			}
		}
	}

	return cluster, namespaced, nil
}

// rulesAllowPrune checks whether the given resource rules allow pruning the
// given group/resource
func rulesAllowPrune(rules []authorizationv1.ResourceRule, group, resource string) bool {
	for _, verb := range []string{"get", "list", "delete"} {
		if !rulesAllowVerb(rules, group, resource, verb) {
			return false
		}
	}

	return true
}

// rulesAllowVerb checks whether the given resource rules allow 'verb' on the
// given group/resource
func rulesAllowVerb(rules []authorizationv1.ResourceRule, group, resource, verb string) bool {
	for _, rule := range rules {
		if !match(rule.APIGroups, group) {
			continue
		}

		if !match(rule.Resources, resource) {
			continue
		}

		if !match(rule.Verbs, verb) {
			continue
		}

		if !resourceNamesAllowAll(rule.ResourceNames) {
			continue
		}

		return true
	}

	return false
}

// match checks that the item is in the list of items, or if one of the
// items in the list is a '*'
func match(items []string, item string) bool {
	for _, i := range items {
		if i == "*" {
			return true
		}
		if i == item {
			return true
		}
	}

	return false
}

// resourceNamesAllowAll ensures that the given resource names allow all names.
// We can't filter out resources by name so we can only prune resources if we
// have access regardless of name.
func resourceNamesAllowAll(names []string) bool {
	for _, n := range names {
		if n == "*" {
			return true
		}
	}
	return len(names) == 0
}

// prunable returns true if a resource can be deleted and isn't a subresource
func prunable(r metav1.APIResource) bool {
	if !strings.Contains(r.Name, "/") {
		for _, v := range r.Verbs {
			if v == "delete" {
				return true
			}
		}
	}
	return false
}

// pluralName returns the plural name of a resource, if found in the API
// resources
func (c *Client) pluralName(gvk schema.GroupVersionKind) (string, error) {
	ar, err := c.clientset.Discovery().ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if err != nil {
		return "", err
	}
	for _, r := range ar.APIResources {
		if r.Kind == gvk.Kind {
			return r.Name, nil
		}
	}
	return "", fmt.Errorf("api resource %s not found", gvk.String())
}
