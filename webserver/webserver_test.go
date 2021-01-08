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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

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
		wbList := []kubeapplierv1alpha1.Waybill{
			{
				TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Waybill"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "main",
					Namespace: "foo",
				},
				Spec: kubeapplierv1alpha1.WaybillSpec{RepositoryPath: pointer.StringPtr("foo")},
			},
			{
				TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Waybill"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "main",
					Namespace: "bar",
				},
				Spec: kubeapplierv1alpha1.WaybillSpec{RepositoryPath: pointer.StringPtr("bar")},
			},
		}

		It("Should keep track of Waybill resources on the server", func() {
			By("Listing all the Waybills in the cluster")
			testEnsureWaybills(wbList)
			Eventually(
				func() []kubeapplierv1alpha1.Waybill {
					testWebServer.result.Lock()
					defer testWebServer.result.Unlock()
					ret := make([]kubeapplierv1alpha1.Waybill, len(testWebServer.result.Waybills))
					for i := range testWebServer.result.Waybills {
						ret[i] = testWebServer.result.Waybills[i]
					}
					return ret
				},
				time.Second*15,
				time.Second,
			).Should(ConsistOf(wbList))

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
			Expect(body).To(MatchJSON(`{"result":  "error", "message": "Cannot find Waybills in namespace 'invalid'"}`))

			v.Set("namespace", wbList[0].Namespace)
			res, err = http.PostForm(fmt.Sprintf("http://localhost:%d/api/v1/forceRun", testWebServer.ListenPort), v)
			Expect(err).To(BeNil())
			body, err = ioutil.ReadAll(res.Body)
			Expect(err).To(BeNil())
			Expect(res.StatusCode).To(Equal(http.StatusOK))
			Expect(body).To(MatchJSON(`{"result":  "success", "message": "Run queued"}`))

			testWebServer.Shutdown()
			close(testRunQueue)

			Expect(testWebServerRequests()).To(Equal([]run.Request{{
				Type:    run.ForcedRun,
				Waybill: &wbList[0],
			}}))
		})

		It("Should render HTML on the root page", func() {
			var res *http.Response
			Eventually(
				func() error {
					r, err := http.Get(fmt.Sprintf("http://localhost:%d/", testWebServer.ListenPort))
					if err != nil {
						return err
					}
					res = r
					return nil
				},
				time.Second*15,
				time.Second,
			).Should(BeNil())

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
func testEnsureWaybills(wbList []kubeapplierv1alpha1.Waybill) {
	for i := range wbList {
		err := testKubeClient.Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: wbList[i].Namespace}})
		if err != nil {
			Expect(errors.IsAlreadyExists(err)).To(BeTrue())
		}
		// The ResourceVersion swapping is to prevent the respective error from
		// Create() which makes it difficult to handle it below.
		rv := wbList[i].ResourceVersion
		wbList[i].ResourceVersion = ""
		err = testKubeClient.Create(context.TODO(), &wbList[i])
		if err != nil && errors.IsAlreadyExists(err) {
			wbList[i].ResourceVersion = rv
			Expect(testKubeClient.UpdateWaybill(context.TODO(), &wbList[i])).To(BeNil())
		} else {
			Expect(err).To(BeNil())
		}
		if wbList[i].Status.LastRun != nil {
			// UpdateStatus changes SelfLink to the status sub-resource but we
			// should revert the change for tests to pass
			selfLink := wbList[i].ObjectMeta.SelfLink
			Expect(testKubeClient.UpdateWaybillStatus(context.TODO(), &wbList[i])).To(BeNil())
			wbList[i].ObjectMeta.SelfLink = selfLink
		}
		// This is a workaround for Equal checks to work below.
		// Apparently, List will return Waybills with TypeMeta but
		// Get and Create (which updates the struct) do not.
		wbList[i].TypeMeta = metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Waybill"}
	}
}
