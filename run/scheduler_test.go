package run

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeapplierv1alpha1 "github.com/utilitywarehouse/kube-applier/apis/kubeapplier/v1alpha1"
)

func testSchedulerDrainRequests(requests <-chan Request) func() []Request {
	ret := []Request{}
	finished := make(chan bool)

	go func() {
		for r := range requests {
			ret = append(ret, r)
		}
		close(finished)
	}()

	return func() []Request {
		<-finished
		return ret
	}
}

var _ = Describe("Scheduler", func() {
	var (
		testRunQueue          chan Request
		testScheduler         Scheduler
		testSchedulerRequests func() []Request
	)

	BeforeEach(func() {
		testRunQueue = make(chan Request)
		testSchedulerRequests = testSchedulerDrainRequests(testRunQueue)
		testScheduler = Scheduler{
			ApplicationPollInterval: time.Second * 5,
			GitPollInterval:         time.Second * 5,
			KubeClient:              testKubeClient,
			Metrics:                 testMetricsClient,
			RepoPath:                "../testdata/manifests",
			RunQueue:                testRunQueue,
		}
		testScheduler.Start()
	})

	Context("When running", func() {
		It("Should keep track of Application resources on the server", func() {
			By("Listing all the Applications in the cluster initially")
			appList := []*kubeapplierv1alpha1.Application{
				{
					TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "main",
						Namespace: "foo",
					},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						RepositoryPath: "foo",
						RunInterval:    5,
					},
				},
			}
			testEnsureApplications(appList)
			Eventually(
				testSchedulerCopyApplicationsMap(&testScheduler),
				time.Second*15,
				time.Second,
			).Should(Equal(testSchedulerExpectedApplicationsMap(appList)))

			lastSyncedAt := time.Now()

			By("Listing all the Applications in the cluster regularly")
			appList = append(appList, &kubeapplierv1alpha1.Application{
				TypeMeta: metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "main",
					Namespace: "bar",
				},
				Spec: kubeapplierv1alpha1.ApplicationSpec{
					RepositoryPath: "bar",
				},
			})
			testEnsureApplications(appList)
			Eventually(
				testSchedulerCopyApplicationsMap(&testScheduler),
				time.Second*15,
				time.Second,
			).Should(Equal(testSchedulerExpectedApplicationsMap(appList)))

			t := time.Second*15 - time.Since(lastSyncedAt)
			if t > 0 {
				fmt.Printf("Sleeping for ~%v to record queued runs\n", t.Truncate(time.Second))
				time.Sleep(t)
			}
			lastSyncedAt = time.Now()

			By("Acknowledging changes in the Application Specs")
			appList[0].Spec.RunInterval = 3600
			appList[0].Status = kubeapplierv1alpha1.ApplicationStatus{
				LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{
					Started:  metav1.NewTime(time.Now()), // this is to prevent an "initial" run to be queued
					Finished: metav1.NewTime(time.Now()), // the rest is for the status subresource to pass validation
					Success:  true,
				},
			}
			// remove the "bar" Application
			testKubeClient.Delete(context.TODO(), appList[1])
			appList = appList[:1]
			testEnsureApplications(appList)
			Eventually(
				testSchedulerCopyApplicationsMap(&testScheduler),
				time.Second*15,
				time.Second,
			).Should(Equal(testSchedulerExpectedApplicationsMap(appList)))

			t = time.Second*15 - time.Since(lastSyncedAt)
			if t > 0 {
				fmt.Printf("Sleeping for ~%v to record queued runs\n", t.Truncate(time.Second))
				time.Sleep(t)
			}

			testScheduler.Stop()

			close(testRunQueue)
			requestCount := map[string]map[Type]int{}
			for _, req := range testSchedulerRequests() {
				if _, ok := requestCount[req.Application.Namespace]; !ok {
					requestCount[req.Application.Namespace] = map[Type]int{}
				}
				requestCount[req.Application.Namespace][req.Type]++
			}
			Expect(requestCount).To(MatchAllKeys(Keys{
				"foo": MatchAllKeys(Keys{
					// RunInterval is 5s and ~15s have elapsed until it is updated to 3600s.
					ScheduledRun: BeNumerically(">=", 4),
				}),
				"bar": MatchAllKeys(Keys{
					// RunInterval is 3600s and then the Application is removed.
					ScheduledRun: Equal(1),
				}),
			}))
		})
	})
})

func testEnsureApplications(appList []*kubeapplierv1alpha1.Application) {
	for i := range appList {
		err := testKubeClient.Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: appList[i].Namespace}})
		if err != nil {
			Expect(errors.IsAlreadyExists(err)).To(BeTrue())
		}
		err = testKubeClient.Create(context.TODO(), appList[i])
		if err != nil {
			Expect(testKubeClient.UpdateApplication(context.TODO(), appList[i])).To(BeNil())
		}
		if appList[i].Status.LastRun != nil {
			// UpdateStatus changes SelfLink to the status sub-resource but we
			// should revert the change for tests to pass
			selfLink := appList[i].ObjectMeta.SelfLink
			Expect(testKubeClient.UpdateApplicationStatus(context.TODO(), appList[i])).To(BeNil())
			appList[i].ObjectMeta.SelfLink = selfLink
		}
		// This is a workaround for Equal checks to work below.
		// Apparently, List will return Applications with TypeMeta but
		// Get and Create (which updates the struct) do not.
		appList[i].TypeMeta = metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"}
	}
}

func testSchedulerExpectedApplicationsMap(appList []*kubeapplierv1alpha1.Application) map[string]*kubeapplierv1alpha1.Application {
	expectedAppMap := map[string]*kubeapplierv1alpha1.Application{}
	for i := range appList {
		expectedAppMap[appList[i].Namespace] = appList[i]
	}
	return expectedAppMap
}

func testSchedulerCopyApplicationsMap(scheduler *Scheduler) func() map[string]*kubeapplierv1alpha1.Application {
	return func() map[string]*kubeapplierv1alpha1.Application {
		scheduler.applicationsMutex.Lock()
		defer scheduler.applicationsMutex.Unlock()
		apps := map[string]*kubeapplierv1alpha1.Application{}
		for i := range scheduler.applications {
			apps[i] = scheduler.applications[i]
		}
		return apps
	}
}
