// Package client defines a custom kubernetes Client for use with kube-applier
// and its custom resources.
package client

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	kubeapplierlog "github.com/utilitywarehouse/kube-applier/log"
	// +kubebuilder:scaffold:imports
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
	client.Client
	clientset kubernetes.Interface
	cfg       *rest.Config
	recorder  record.EventRecorder
}

// New returns a new kubernetes client.
func New() (*Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("Cannot get kubernetes config: %v", err)
	}
	return newClient(cfg)
}

// NewWithConfig returns a new kubernetes client initialised with the provided
// configuration.
func NewWithConfig(cfg *rest.Config) (*Client, error) {
	return newClient(cfg)
}

func newClient(cfg *rest.Config) (*Client, error) {
	c, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("Cannot create default client: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(func(format string, args ...interface{}) {
		kubeapplierlog.Logger("eventBroadcaster").Debug(fmt.Sprintf(format, args...))
	})
	eventBroadcaster.StartRecordingToSink(&clientv1.EventSinkImpl{Interface: clientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme, corev1.EventSource{Component: Name})
	return &Client{
		Client:    c,
		clientset: clientset,
		cfg:       cfg,
		recorder:  recorder,
	}, nil
}

// WithToken returns a copy of the Client that will perform actions using the
// provided token.
func (c *Client) WithToken(token string) (*Client, error) {
	cfg := rest.AnonymousClientConfig(c.cfg)
	cfg.BearerToken = token
	return newClient(cfg)
}

// EmitWaybillEvent creates an Event for the provided Waybill.
func (c *Client) EmitWaybillEvent(waybill *kubeapplierv1alpha1.Waybill, eventType, reason, messageFmt string, args ...interface{}) {
	c.recorder.Eventf(waybill, eventType, reason, messageFmt, args...)
}

// ListWaybills returns a list of all the Waybill resources.
func (c *Client) ListWaybills(ctx context.Context) ([]kubeapplierv1alpha1.Waybill, error) {
	waybills := &kubeapplierv1alpha1.WaybillList{}
	if err := c.List(ctx, waybills); err != nil {
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
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, waybill); err != nil {
		return nil, err
	}
	return waybill, nil
}

// UpdateWaybill updates the Waybill resource provided.
func (c *Client) UpdateWaybill(ctx context.Context, waybill *kubeapplierv1alpha1.Waybill) error {
	return c.Update(ctx, waybill, defaultUpdateOptions)
}

// UpdateWaybillStatus updates the status of the Waybill resource
// provided.
func (c *Client) UpdateWaybillStatus(ctx context.Context, waybill *kubeapplierv1alpha1.Waybill) error {
	return c.Status().Update(ctx, waybill, defaultUpdateOptions)
}

// GetSecret returns the Secret resource specified by the namespace and name.
func (c *Client) GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// PrunableResourceGVKs returns cluster and namespaced resources as two slices of
// strings of the format <group>/<version>/<kind>. It only returns resources
// that support pruning.
func (c *Client) PrunableResourceGVKs() ([]string, []string, error) {
	var cluster, namespaced []string

	_, resourceList, err := c.clientset.Discovery().ServerGroupsAndResources()
	if err != nil {
		return cluster, namespaced, err
	}

	for _, l := range resourceList {
		groupVersion := l.GroupVersion
		if groupVersion == "v1" {
			groupVersion = "core/v1"
		}

		for _, r := range l.APIResources {
			if prunable(r) {
				gvk := groupVersion + "/" + r.Kind
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
