package kube

import (
	"context"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	// support any type of auth in kubeconfig
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	enabledAnnotation               = "kube-applier.io/enabled"
	dryRunAnnotation                = "kube-applier.io/dry-run"
	pruneAnnotation                 = "kube-applier.io/prune"
	pruneClusterResourcesAnnotation = "kube-applier.io/prune-cluster-resources"
	serverSideAnnotation            = "kube-applier.io/server-side"
)

// KAAnnotations contains the standard set of annotations on the Namespace
// resource defining behaviour for that Namespace
type KAAnnotations struct {
	Enabled               string
	DryRun                string
	Prune                 string
	PruneClusterResources string
	ServerSide            string
}

// ClientInterface allows for mocking out the functionality of Client when testing the full process of an apply run.
type ClientInterface interface {
	NamespaceAnnotations(namespace string) (KAAnnotations, error)
	PrunableResourceGVKs() ([]string, []string, error)
}

// Client interacts with the kubernetes API via client-go
type Client struct {
	clientset kubernetes.Interface
}

// New returns a new client
func New() (*Client, error) {
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &Client{
		clientset: clientset,
	}, nil
}

// NamespaceAnnotations returns string values of kube-applier annotations
func (c *Client) NamespaceAnnotations(namespace string) (KAAnnotations, error) {
	kaa := KAAnnotations{}

	ns, err := c.clientset.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	if err != nil {
		return kaa, err
	}

	kaa.Enabled = ns.Annotations[enabledAnnotation]
	kaa.DryRun = ns.Annotations[dryRunAnnotation]
	kaa.Prune = ns.Annotations[pruneAnnotation]
	kaa.PruneClusterResources = ns.Annotations[pruneClusterResourcesAnnotation]
	kaa.ServerSide = ns.Annotations[serverSideAnnotation]

	return kaa, nil
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
