package client

import (
	"context"
	"fmt"
	"log"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	// +kubebuilder:scaffold:imports
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
	return &Client{
		Client:    c,
		clientset: clientset,
	}, nil
}

// ListApplications returns a list of all the Application resources.
func (c *Client) ListApplications(ctx context.Context) ([]kubeapplierv1alpha1.Application, error) {
	apps := &kubeapplierv1alpha1.ApplicationList{}
	if err := c.List(ctx, apps); err != nil {
		return nil, err
	}
	return apps.Items, nil
}

// GetApplication returns the Application resource specified by the namespace
// and name.
func (c *Client) GetApplication(ctx context.Context, namespace, name string) (*kubeapplierv1alpha1.Application, error) {
	app := &kubeapplierv1alpha1.Application{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, app); err != nil {
		return nil, err
	}
	return app, nil
}

// UpdateApplication updates the Application resource provided.
func (c *Client) UpdateApplication(ctx context.Context, app *kubeapplierv1alpha1.Application) error {
	return c.Update(ctx, app, defaultUpdateOptions)
}

// UpdateApplicationStatus updates the status of the Application resource
// provided.
func (c *Client) UpdateApplicationStatus(ctx context.Context, app *kubeapplierv1alpha1.Application) error {
	return c.Status().Update(ctx, app, defaultUpdateOptions)
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
