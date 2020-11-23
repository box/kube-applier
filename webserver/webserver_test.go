package webserver

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	//. "github.com/onsi/gomega/gstruct"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
	"github.com/utilitywarehouse/kube-applier/run"
)

// TODO: this is essentially duplication from the run package, can we share?
func testWebServerDrainRequests(requests <-chan run.Request) func() []run.Request {
	ret := []run.Request{}
	finished := make(chan bool)

	go func() {
		for r := range requests {
			ret = append(ret, r)
		}
		close(finished)
	}()

	return func() []run.Request {
		<-finished
		return ret
	}
}

var _ = Describe("WebServer", func() {
	var (
		testRunQueue          chan run.Request
		testWebServer         WebServer
		testWebServerRequests func() []run.Request
	)

	BeforeEach(func() {
		testRunQueue = make(chan run.Request)
		testWebServerRequests = testWebServerDrainRequests(testRunQueue)
		testWebServer = WebServer{
			ListenPort:           35432,
			Clock:                &zeroClock{},
			DiffURLFormat:        "http://foo.bar/diff/%s",
			KubeClient:           testKubeClient,
			RunQueue:             testRunQueue,
			StatusUpdateInterval: time.Second * 5,
			TemplatePath:         "../templates/status.html",
		}
		Expect(testWebServer.Start()).To(BeNil())
	})

	Context("When running", func() {
		appList := []kubeapplierv1alpha1.Application{
			{
				TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "main",
					Namespace: "foo",
				},
				Spec: kubeapplierv1alpha1.ApplicationSpec{RepositoryPath: "foo"},
			},
			{
				TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "main",
					Namespace: "bar",
				},
				Spec: kubeapplierv1alpha1.ApplicationSpec{RepositoryPath: "bar"},
			},
		}

		It("Should keep track of Application resources on the server", func() {
			By("Listing all the Applications in the cluster")
			testEnsureApplications(appList)
			Eventually(
				func() []kubeapplierv1alpha1.Application {
					testWebServer.result.Lock()
					defer testWebServer.result.Unlock()
					ret := make([]kubeapplierv1alpha1.Application, len(testWebServer.result.Applications))
					for i := range testWebServer.result.Applications {
						ret[i] = testWebServer.result.Applications[i]
					}
					return ret
				},
				time.Second*15,
				time.Second,
			).Should(ConsistOf(appList))

			testWebServer.Shutdown()
			close(testRunQueue)

			Expect(testWebServerRequests()).To(Equal([]run.Request{}))
		})

		It("Should trigger a ForcedRun when a valid request is made", func() {
			v := url.Values{}
			res, err := http.Get(fmt.Sprintf("http://localhost:%d/api/v1/forceRun", testWebServer.ListenPort))
			Expect(err).To(BeNil())
			body, err := ioutil.ReadAll(res.Body)
			Expect(err).To(BeNil())
			Expect(res.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(body).To(MatchJSON(`{"result":  "error", "message": "Must be a POST request"}`))

			res, err = http.PostForm(fmt.Sprintf("http://localhost:%d/api/v1/forceRun", testWebServer.ListenPort), v)
			Expect(err).To(BeNil())
			body, err = ioutil.ReadAll(res.Body)
			Expect(err).To(BeNil())
			Expect(res.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(body).To(MatchJSON(`{"result":  "error", "message": "Empty namespace value"}`))

			v.Set("namespace", "invalid")
			res, err = http.PostForm(fmt.Sprintf("http://localhost:%d/api/v1/forceRun", testWebServer.ListenPort), v)
			Expect(err).To(BeNil())
			body, err = ioutil.ReadAll(res.Body)
			Expect(err).To(BeNil())
			Expect(res.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(body).To(MatchJSON(`{"result":  "error", "message": "Cannot find Applications in namespace 'invalid'"}`))

			v.Set("namespace", appList[0].Namespace)
			res, err = http.PostForm(fmt.Sprintf("http://localhost:%d/api/v1/forceRun", testWebServer.ListenPort), v)
			Expect(err).To(BeNil())
			body, err = ioutil.ReadAll(res.Body)
			Expect(err).To(BeNil())
			Expect(res.StatusCode).To(Equal(http.StatusOK))
			Expect(body).To(MatchJSON(`{"result":  "success", "message": "Run queued"}`))

			testWebServer.Shutdown()
			close(testRunQueue)

			Expect(testWebServerRequests()).To(Equal([]run.Request{{
				Type:        run.ForcedRun,
				Application: &appList[0],
			}}))
		})

		It("Should render HTML on the root page", func() {
			res, err := http.Get(fmt.Sprintf("http://localhost:%d/", testWebServer.ListenPort))
			Expect(err).To(BeNil())
			body, err := ioutil.ReadAll(res.Body)
			Expect(err).To(BeNil())
			Expect(res.StatusCode).To(Equal(http.StatusOK))
			Expect(body).ToNot(BeEmpty())

			testWebServer.Shutdown()
			close(testRunQueue)
			Expect(testWebServerRequests()).To(Equal([]run.Request{}))
		})
	})
})

// TODO: this is essentially duplication from the run package (but with values
// instead of pointers), can we share?
func testEnsureApplications(appList []kubeapplierv1alpha1.Application) {
	for i := range appList {
		err := testKubeClient.Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: appList[i].Namespace}})
		if err != nil {
			Expect(errors.IsAlreadyExists(err)).To(BeTrue())
		}
		err = testKubeClient.Create(context.TODO(), &appList[i])
		if err != nil {
			Expect(testKubeClient.UpdateApplication(context.TODO(), &appList[i])).To(BeNil())
		}
		if appList[i].Status.LastRun != nil {
			// UpdateStatus changes SelfLink to the status sub-resource but we
			// should revert the change for tests to pass
			selfLink := appList[i].ObjectMeta.SelfLink
			Expect(testKubeClient.UpdateApplicationStatus(context.TODO(), &appList[i])).To(BeNil())
			appList[i].ObjectMeta.SelfLink = selfLink
		}
		// This is a workaround for Equal checks to work below.
		// Apparently, List will return Applications with TypeMeta but
		// Get and Create (which updates the struct) do not.
		appList[i].TypeMeta = metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"}
	}
}
