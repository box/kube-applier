package run

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	gomegatypes "github.com/onsi/gomega/types"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	controllerruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/client"
	"github.com/utilitywarehouse/kube-applier/log"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var testConfig *rest.Config
var testKubeClient *client.Client
var testEnv *envtest.Environment

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Run package suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "manifests", "base", "cluster")},
	}

	var err error
	testConfig, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(testConfig).ToNot(BeNil())

	err = kubeapplierv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	testKubeClient, err = client.NewWithConfig(testConfig)
	Expect(err).ToNot(HaveOccurred())
	Expect(testKubeClient).ToNot(BeNil())

	hostParts := strings.Split(testConfig.Host, ":")
	os.Setenv("KUBERNETES_SERVICE_HOST", hostParts[0])
	os.Setenv("KUBERNETES_SERVICE_PORT", hostParts[1])

	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

func init() {
	log.SetLevel("off")
}

type zeroClock struct{}

func (c *zeroClock) Now() time.Time                  { return time.Time{} }
func (c *zeroClock) Since(t time.Time) time.Duration { return time.Duration(0) }
func (c *zeroClock) Sleep(d time.Duration)           {}

// testMetrics spins up a temporary webserver that exports the metrics and
// captures the response to be tested again regexes
func testMetrics(regex []string) {
	server := &http.Server{
		Addr:    fmt.Sprintf(":12700"),
		Handler: promhttp.Handler(),
	}
	go server.ListenAndServe()
	defer server.Shutdown(context.TODO())
	var output string
	Eventually(
		func() error {
			res, err := http.Get(fmt.Sprintf("http://%s", server.Addr))
			if err != nil {
				return err
			}
			body, err := io.ReadAll(res.Body)
			if err != nil {
				return err
			}
			output = string(body)
			return nil
		},
		time.Second*15,
		time.Second,
	).Should(BeNil())
	// remove any metrics that don't come from the metrics package to reduce
	// output length in case of failures
	metricsLines := []string{}
	for _, s := range strings.Split(output, "\n") {
		if strings.HasPrefix(s, "kube_applier") {
			metricsLines = append(metricsLines, s)
		}
	}
	output = strings.Join(metricsLines, "\n")
	matchers := make([]gomegatypes.GomegaMatcher, len(regex))
	for i, r := range regex {
		matchers[i] = MatchRegexp(r)
	}
	Expect(output).To(And(matchers...))
}

func testCleanupNamespaces() {
	// With the envtest package we cannot delete namespaces, however, deleting
	// the CRs should be enough to avoid test pollution.
	// See https://github.com/kubernetes-sigs/controller-runtime/issues/880
	testRemoveAllWaybills()
}

func testRemoveAllWaybills() {
	// Although we could in theory use DeleteAllOf() here, it returns with a
	// NotFound error that has proven hard to debug. Instead, we can manually
	// List and Delete Waybills one by one. There should not be too many of them
	// to significantly affect test duration.
	waybills := kubeapplierv1alpha1.WaybillList{}
	Expect(testKubeClient.List(
		context.TODO(),
		&waybills,
	)).To(BeNil())
	for _, wb := range waybills.Items {
		Expect(testKubeClient.Delete(
			context.TODO(),
			&wb,
			controllerruntimeclient.GracePeriodSeconds(0),
		)).To(BeNil())
	}
	Eventually(
		func() int {
			waybills := kubeapplierv1alpha1.WaybillList{}
			Expect(testKubeClient.List(context.TODO(), &waybills)).To(BeNil())
			return len(waybills.Items)
		},
		time.Second*60,
		time.Second,
	).Should(Equal(0))
}

func testMatchEvents(matchers []gomegatypes.GomegaMatcher) {
	elements := make([]interface{}, len(matchers))
	for i := range matchers {
		elements[i] = matchers[i]
	}
	Eventually(
		func() ([]corev1.Event, error) {
			events := &corev1.EventList{}
			if err := testKubeClient.List(context.TODO(), events); err != nil {
				return nil, err
			}
			return events.Items, nil
		},
		time.Second*15,
		time.Second,
	).Should(ContainElements(elements...))
}

// matchEvent is duplicated from the client package.
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
			"Component": Equal(client.Name),
		}),
		"Type": Equal(eventType),
	})
}
