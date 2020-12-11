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
	"github.com/utilitywarehouse/kube-applier/git"
	"github.com/utilitywarehouse/kube-applier/metrics"
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
			Clock:                   &zeroClock{},
			GitPollInterval:         time.Second * 5,
			KubeClient:              testKubeClient,
			RepoPath:                "../testdata/manifests",
			RunQueue:                testRunQueue,
		}
		testScheduler.Start()

		metrics.Reset()
	})

	AfterEach(func() {
		testScheduler.Stop()
		testCleanupNamespaces()
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
			testWaitForSchedulerToUpdate(&testScheduler, appList)

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
			testWaitForSchedulerToUpdate(&testScheduler, appList)

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
			testWaitForSchedulerToUpdate(&testScheduler, appList)

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

		It("Should trigger runs for Applications that have had their source change in git", func() {
			gitUtil := &git.Util{RepoPath: "../testdata/manifests"}
			headHash, err := gitUtil.HeadHashForPaths(".")
			Expect(err).To(BeNil())
			Expect(headHash).ToNot(BeEmpty())
			appAHeadHash, err := gitUtil.HeadHashForPaths("app-a")
			Expect(err).To(BeNil())
			Expect(appAHeadHash).ToNot(BeEmpty())
			appAKHeadHash, err := gitUtil.HeadHashForPaths("app-a-kustomize")
			Expect(err).To(BeNil())
			Expect(appAKHeadHash).ToNot(BeEmpty())

			appList := []*kubeapplierv1alpha1.Application{
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "ignored"},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						RepositoryPath: "ignored",
					},
					Status: kubeapplierv1alpha1.ApplicationStatus{
						LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{
							Finished: metav1.NewTime(time.Now()),
							Started:  metav1.NewTime(time.Now()),
						},
					},
				},
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "up-to-date"},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						RepositoryPath: "up-to-date",
					},
					Status: kubeapplierv1alpha1.ApplicationStatus{
						LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{
							Finished: metav1.NewTime(time.Now()),
							Started:  metav1.NewTime(time.Now()),
							Commit:   headHash,
						},
					},
				},
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "app-a"},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						RepositoryPath: "app-a",
					},
					Status: kubeapplierv1alpha1.ApplicationStatus{
						LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{
							Finished: metav1.NewTime(time.Now()),
							Started:  metav1.NewTime(time.Now()),
							Commit:   appAHeadHash, // this is the app-a dir head hash, no changes since
						},
					},
				},
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "app-a-kustomize"},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						RepositoryPath: "app-a-kustomize",
					},
					Status: kubeapplierv1alpha1.ApplicationStatus{
						LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{
							Finished: metav1.NewTime(time.Now()),
							Started:  metav1.NewTime(time.Now()),
							Commit:   fmt.Sprintf("%s^1", appAKHeadHash), // this is a hack that should always return changes
						},
					},
				},
			}
			testEnsureApplications(appList)
			testWaitForSchedulerToUpdate(&testScheduler, appList)

			t := time.Second * 10
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
				"app-a-kustomize": MatchAllKeys(Keys{
					PollingRun: Equal(1),
				}),
			}))
		})

		It("Should export metrics about resources applied", func() {
			By("Listing all the Applications in the cluster")
			// The status sub-resource only contains the Output field and this
			// is is only used to test that metrics are properly exported
			appList := []*kubeapplierv1alpha1.Application{
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "metrics-foo"},
					Status: kubeapplierv1alpha1.ApplicationStatus{
						LastRun: &kubeapplierv1alpha1.ApplicationStatusRun{
							Finished: metav1.NewTime(time.Now()),
							Started:  metav1.NewTime(time.Now()),
							Output: `namespace/metrics-foo created
deployment.apps/test-a created (server dry run)
deployment.apps/test-b unchanged
deployment.apps/test-c configured
error: error validating "../testdata/manifests/app-d/deployment.yaml": error validating data: invalid object to validate; if you choose to ignore these errors, turn validation off with --validate=false
Some error output has been omitted because it may contain sensitive data
`,
						},
					},
				},
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "metrics-bar"},
				},
			}
			testEnsureApplications(appList)
			testWaitForSchedulerToUpdate(&testScheduler, appList)

			testScheduler.Stop()
			close(testRunQueue)

			By("Parsing the Output field in the Application status and exporting metrics about individual resources")
			testMetrics([]string{
				`kube_applier_result_summary{action="created",name="metrics-foo",namespace="metrics-foo",type="namespace"} 1`,
				`kube_applier_result_summary{action="created",name="test-a",namespace="metrics-foo",type="deployment.apps"} 1`,
				`kube_applier_result_summary{action="unchanged",name="test-b",namespace="metrics-foo",type="deployment.apps"} 1`,
				`kube_applier_result_summary{action="configured",name="test-c",namespace="metrics-foo",type="deployment.apps"} 1`,
			})
		})

		It("Should export Application spec metrics from the cluster state", func() {
			appList := []*kubeapplierv1alpha1.Application{
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "spec-foo"},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						RepositoryPath: "foo",
						RunInterval:    5,
					},
				},
				{
					TypeMeta:   metav1.TypeMeta{APIVersion: "kube-applier.io/v1alpha1", Kind: "Application"},
					ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "spec-bar"},
					Spec: kubeapplierv1alpha1.ApplicationSpec{
						RepositoryPath: "bar",
						DryRun:         true,
					},
				},
			}
			testEnsureApplications(appList)
			testWaitForSchedulerToUpdate(&testScheduler, appList)

			testScheduler.Stop()
			close(testRunQueue)

			testMetrics([]string{
				`kube_applier_application_spec_dry_run{namespace="spec-foo"} 0`,
				`kube_applier_application_spec_run_interval{namespace="spec-foo"} 5`,
				`kube_applier_application_spec_dry_run{namespace="spec-bar"} 1`,
				`kube_applier_application_spec_run_interval{namespace="spec-bar"} 3600`,
			})
		})
	})
})

func testEnsureApplications(appList []*kubeapplierv1alpha1.Application) {
	for i := range appList {
		err := testKubeClient.Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: appList[i].Namespace}})
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).To(BeNil())
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

func testWaitForSchedulerToUpdate(s *Scheduler, appList []*kubeapplierv1alpha1.Application) {
	Eventually(
		testSchedulerCopyApplicationsMap(s),
		time.Second*15,
		time.Second,
	).Should(Equal(testSchedulerExpectedApplicationsMap(appList)))
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
