package client

import (
	"context"
	"fmt"
	"log"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	// +kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
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

type Client struct {
	client.Client
}

func New() (*Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("Cannot get kubernetes config: %v", err)
	}
	c, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("Cannot create default client: %v", err)
	}
	return &Client{c}, nil
}

func (c *Client) ListApplications(ctx context.Context) ([]kubeapplierv1alpha1.Application, error) {
	apps := &kubeapplierv1alpha1.ApplicationList{}
	if err := c.List(ctx, apps); err != nil {
		return nil, err
	}
	return apps.Items, nil
}

func (c *Client) GetApplication(ctx context.Context, key client.ObjectKey) (*kubeapplierv1alpha1.Application, error) {
	app := &kubeapplierv1alpha1.Application{}
	if err := c.Get(ctx, key, app); err != nil {
		return nil, err
	}
	return app, nil
}
