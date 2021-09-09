package client

import (
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/log"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	testConfig     *rest.Config
	testKubeClient *Client
	testEnv        *envtest.Environment
)

func TestClient(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Run package suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "manifests", "base", "cluster")},
	}

	var err error
	testConfig, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(testConfig).ToNot(BeNil())

	err = kubeapplierv1alpha1.AddToScheme(kubescheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	testKubeClient, err = NewWithConfig(testConfig)
	Expect(err).ToNot(HaveOccurred())
	Expect(testKubeClient).ToNot(BeNil())
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	testKubeClient.Shutdown()
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

func init() {
	log.SetLevel("off")
}
