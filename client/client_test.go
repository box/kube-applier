package client

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	gomegatypes "github.com/onsi/gomega/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
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

var _ = Describe("Client", func() {
	Context("When listing applications", func() {
		It("Should return only one Application per namespace and emit events for the others", func() {
			appList := []kubeapplierv1alpha1.Application{
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: "ns-0"},
				},
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{Name: "beta", Namespace: "ns-0"},
				},
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "ns-1"},
				},
			}

			for i := range appList {
				err := testKubeClient.Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: appList[i].Namespace}})
				if err != nil {
					Expect(errors.IsAlreadyExists(err)).To(BeTrue())
				}
				Expect(testKubeClient.Create(context.TODO(), &appList[i])).To(BeNil())
			}

			Eventually(
				func() int {
					apps, err := testKubeClient.ListApplications(context.TODO())
					if err != nil {
						return -1
					}
					return len(apps)
				},
				time.Second*15,
				time.Second,
			).Should(Equal(2))

			events := &corev1.EventList{}
			Eventually(
				func() int {
					if err := testKubeClient.List(context.TODO(), events); err != nil {
						return -1
					}
					return len(events.Items)
				},
				time.Second*15,
				time.Second,
			).Should(Equal(1))
			for _, e := range events.Items {
				Expect(e).To(matchEvent(appList[1], corev1.EventTypeWarning, "MultipleApplicationsFound", fmt.Sprintf("^.*%s.*$", appList[0].Name)))
			}

			Expect(testKubeClient.Delete(context.TODO(), &events.Items[0])).To(BeNil())
		})
	})
})

func matchEvent(app kubeapplierv1alpha1.Application, eventType, reason, message string) gomegatypes.GomegaMatcher {
	return MatchFields(IgnoreExtras, Fields{
		"TypeMeta": Ignore(),
		"ObjectMeta": MatchFields(IgnoreExtras, Fields{
			"Namespace": Equal(app.ObjectMeta.Namespace),
		}),
		"InvolvedObject": MatchFields(IgnoreExtras, Fields{
			"Kind":      Equal("Application"),
			"Namespace": Equal(app.ObjectMeta.Namespace),
			"Name":      Equal(app.ObjectMeta.Name),
		}),
		"Action":  BeEmpty(),
		"Count":   BeNumerically(">", 0),
		"Message": MatchRegexp(message),
		"Reason":  Equal(reason),
		"Source": MatchFields(IgnoreExtras, Fields{
			"Component": Equal(clientName),
		}),
		"Type": Equal(eventType),
	})
}
