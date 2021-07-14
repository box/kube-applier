package client

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	gomegatypes "github.com/onsi/gomega/types"
	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var _ = Describe("Client", func() {
	Context("When retrieving prunable resources", func() {
		It("Should only return resources that support delete and that the client has permissions to get/list/delete", func() {
			// Create a user and a client that can auth as the user
			user, err := testEnv.AddUser(envtest.User{Name: "foobar"}, testConfig)
			Expect(err).NotTo(HaveOccurred())
			userKubeClient, err := NewWithConfig(user.Config())
			Expect(userKubeClient).ToNot(BeNil())

			// Create a namespace for the user to manage
			if err := testKubeClient.Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "foobar"}}); err != nil {
				Expect(errors.IsAlreadyExists(err)).To(BeTrue())
			}

			// Create a clusterrole that gives the user access to
			// various cluster/namespaced resources
			if err := testKubeClient.Create(context.TODO(), &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: "foobar"},
				Rules: []rbacv1.PolicyRule{
					{
						Verbs:     []string{"*"},
						APIGroups: []string{""},
						Resources: []string{"pods"},
					},
					{
						Verbs:     []string{"*"},
						APIGroups: []string{""},
						Resources: []string{"namespaces"},
					},
					{
						Verbs:     []string{"get", "list", "delete"},
						APIGroups: []string{"storage.k8s.io"},
						Resources: []string{"storageclasses"},
					},
					{
						Verbs:     []string{"get", "list", "delete"},
						APIGroups: []string{"apps"},
						Resources: []string{"deployments"},
					},
					// Not prunable: get, list and delete
					// permissions are required to prune a
					// resource
					{
						Verbs:     []string{"delete"},
						APIGroups: []string{""},
						Resources: []string{"serviceaccounts"},
					},
					// Not prunable: pruning individual
					// resources by name isn't possible, so
					// we can't support specific
					// ResourceNames
					{
						Verbs:         []string{"*"},
						APIGroups:     []string{""},
						Resources:     []string{"validatingwebhookconfigurations"},
						ResourceNames: []string{"foobar"},
					},
					// Not prunable: bindings don't support
					// the 'delete' verb
					{
						Verbs:     []string{"*"},
						APIGroups: []string{""},
						Resources: []string{"bindings"},
					},
				}}); err != nil {
				Expect(errors.IsAlreadyExists(err)).To(BeTrue())
			}
			if err := testKubeClient.Create(context.TODO(), &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "foobar", Namespace: "foobar"},
				Subjects: []rbacv1.Subject{
					{
						Kind: "User",
						Name: "foobar",
					},
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "foobar",
				}}); err != nil {
				Expect(errors.IsAlreadyExists(err)).To(BeTrue())
			}

			// Ensure that only prunable resources are returned
			cluster, namespaced, err := userKubeClient.PrunableResourceGVKs(context.TODO(), "foobar")
			Expect(err).NotTo(HaveOccurred())
			Expect(cluster).To(Equal([]string{
				"core/v1/Namespace",
				"storage.k8s.io/v1/StorageClass",
				"storage.k8s.io/v1beta1/StorageClass",
			}))
			Expect(namespaced).To(Equal([]string{
				"core/v1/Pod",
				"apps/v1/Deployment",
			}))
		})
	})
	Context("When listing waybills", func() {
		It("Should return only one Waybill per namespace and emit events for the others", func() {
			wbList := []kubeapplierv1alpha1.Waybill{
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Waybill"},
					ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: "ns-0"},
				},
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Waybill"},
					ObjectMeta: metav1.ObjectMeta{Name: "beta", Namespace: "ns-0"},
				},
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Waybill"},
					ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "ns-1"},
				},
			}

			for i := range wbList {
				err := testKubeClient.Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: wbList[i].Namespace}})
				if err != nil {
					Expect(errors.IsAlreadyExists(err)).To(BeTrue())
				}
				Expect(testKubeClient.Create(context.TODO(), &wbList[i])).To(BeNil())
			}

			Eventually(
				func() int {
					waybills, err := testKubeClient.ListWaybills(context.TODO())
					if err != nil {
						return -1
					}
					return len(waybills)
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
				Expect(e).To(matchEvent(wbList[1], corev1.EventTypeWarning, "MultipleWaybillsFound", fmt.Sprintf("^.*%s.*$", wbList[0].Name)))
			}

			Expect(testKubeClient.Delete(context.TODO(), &events.Items[0])).To(BeNil())
		})
	})
})

func matchEvent(waybill kubeapplierv1alpha1.Waybill, eventType, reason, message string) gomegatypes.GomegaMatcher {
	return MatchFields(IgnoreExtras, Fields{
		"TypeMeta": Ignore(),
		"ObjectMeta": MatchFields(IgnoreExtras, Fields{
			"Namespace": Equal(waybill.ObjectMeta.Namespace),
		}),
		"InvolvedObject": MatchFields(IgnoreExtras, Fields{
			"Kind":      Equal("Waybill"),
			"Namespace": Equal(waybill.ObjectMeta.Namespace),
			"Name":      Equal(waybill.ObjectMeta.Name),
		}),
		"Action":  BeEmpty(),
		"Count":   BeNumerically(">", 0),
		"Message": MatchRegexp(message),
		"Reason":  Equal(reason),
		"Source": MatchFields(IgnoreExtras, Fields{
			"Component": Equal(Name),
		}),
		"Type": Equal(eventType),
	})
}
