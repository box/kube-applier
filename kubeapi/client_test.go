package kubeapi

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPrunableResourceGVKs(t *testing.T) {
	fake := fake.NewSimpleClientset()

	fake.Resources = []*metav1.APIResourceList{
		&metav1.APIResourceList{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				metav1.APIResource{
					Name:       "pods",
					Namespaced: true,
					Kind:       "Pod",
					Verbs: metav1.Verbs{
						"create",
						"delete",
						"deletecollection",
						"get",
						"list",
						"patch",
						"update",
						"watch",
					},
					ShortNames:         []string{"po"},
					Categories:         []string{"all"},
					StorageVersionHash: "xPOwRZ+Yhw8=",
				},
				metav1.APIResource{
					Name:       "pods/proxy",
					Namespaced: true,
					Kind:       "PodProxyOptions",
					Verbs: metav1.Verbs{
						"create",
						"delete",
						"get",
						"patch",
						"update",
					},
				},
				metav1.APIResource{
					Name:       "namespaces",
					Namespaced: false,
					Kind:       "Namespace",
					Verbs: metav1.Verbs{
						"create",
						"delete",
						"get",
						"list",
						"patch",
						"update",
						"watch",
					},
					ShortNames:         []string{"ns"},
					StorageVersionHash: "Q3oi5N2YM8M=",
				},
				metav1.APIResource{
					Name:       "bindings",
					Namespaced: true,
					Kind:       "Binding",
					Verbs: metav1.Verbs{
						"create",
					},
				},
			},
		},
		&metav1.APIResourceList{
			GroupVersion: "apps/v1",
			APIResources: []metav1.APIResource{
				metav1.APIResource{
					Name:       "deployments",
					Namespaced: true,
					Kind:       "Deployment",
					Verbs: metav1.Verbs{
						"create",
						"delete",
						"deletecollection",
						"get",
						"list",
						"patch",
						"update",
						"watch",
					},
					ShortNames:         []string{"deploy"},
					Categories:         []string{"all"},
					StorageVersionHash: "8aSe+NMegvE=",
				},
			},
		},
		&metav1.APIResourceList{
			GroupVersion: "storage.k8s.io/v1beta1",
			APIResources: []metav1.APIResource{
				metav1.APIResource{
					Name:       "storageclasses",
					Namespaced: false,
					Kind:       "StorageClass",
					Verbs: metav1.Verbs{
						"create",
						"delete",
						"deletecollection",
						"get",
						"list",
						"patch",
						"update",
						"watch",
					},
					ShortNames:         []string{"sc"},
					StorageVersionHash: "K+m6uJwbjGY=",
				},
			},
		},
		&metav1.APIResourceList{
			GroupVersion: "storage.k8s.io/v1",
			APIResources: []metav1.APIResource{
				metav1.APIResource{
					Name:       "storageclasses",
					Namespaced: false,
					Kind:       "StorageClass",
					Verbs: metav1.Verbs{
						"create",
						"delete",
						"deletecollection",
						"get",
						"list",
						"patch",
						"update",
						"watch",
					},
					ShortNames:         []string{"sc"},
					StorageVersionHash: "K+m6uJwbjGY=",
				},
			},
		},
	}

	client := &Client{
		clientset: fake,
	}

	cluster, namespaced, err := client.PrunableResourceGVKs()
	if err != nil {
		t.Fatalf("Unexpected error returned by PrunableResourceGVKs: %s", err)
	}

	clusterWant := []string{
		"core/v1/Namespace",
		"storage.k8s.io/v1beta1/StorageClass",
		"storage.k8s.io/v1/StorageClass",
	}
	if !reflect.DeepEqual(cluster, clusterWant) {
		t.Errorf("Unexpected cluster resources; got %v want %v", cluster, clusterWant)

	}

	namespacedWant := []string{"core/v1/Pod", "apps/v1/Deployment"}
	if !reflect.DeepEqual(namespaced, namespacedWant) {
		t.Errorf("Unexpected namespaced resources; got %v want %v", namespaced, namespacedWant)
	}
}

func TestPrunable(t *testing.T) {
	resource := metav1.APIResource{
		Name: "pods",
		Verbs: []string{
			"create",
			"delete",
			"deletecollection",
			"get",
			"list",
			"patch",
			"update",
			"watch",
		},
	}
	if !prunable(resource) {
		t.Errorf("Expected prunable to return true but got false for resource: %v", resource)
	}
}

func TestPrunableSubresource(t *testing.T) {
	resource := metav1.APIResource{
		Name: "pods/proxy",
		Verbs: []string{
			"create",
			"delete",
			"get",
			"patch",
			"update",
		},
	}
	if prunable(resource) {
		t.Errorf("Expected prunable to return false but got true for resource: %v", resource)
	}
}

func TestPrunableNoDelete(t *testing.T) {
	resource := metav1.APIResource{
		Name: "bindings",
		Verbs: []string{
			"create",
		},
	}
	if prunable(resource) {
		t.Errorf("Expected prunable to return false but got true for resource: %v", resource)
	}
}
