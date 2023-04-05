package daemonset

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	kruiseappsv1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	"github.com/openkruise/rollouts/api/v1alpha1"
	batchcontext "github.com/openkruise/rollouts/pkg/controller/batchrelease/context"
	"github.com/openkruise/rollouts/pkg/util"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	scheme = runtime.NewScheme()

	daemonKey = types.NamespacedName{
		Namespace: "default",
		Name:      "daemonset",
	}
	daemonDemo = &kruiseappsv1alpha1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps.kruise.io/v1alpha1",
			Kind:       "DaemonSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       daemonKey.Name,
			Namespace:  daemonKey.Namespace,
			Generation: 1,
			Labels: map[string]string{
				"app": "busybox",
			},
			Annotations: map[string]string{
				"type": "unit-test",
			},
		},
		Spec: kruiseappsv1alpha1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "busybox",
				},
			},

			UpdateStrategy: kruiseappsv1alpha1.DaemonSetUpdateStrategy{
				Type: "RollingUpdate",
				RollingUpdate: &kruiseappsv1alpha1.RollingUpdateDaemonSet{
					Paused:         pointer.Bool(true),
					Partition:      pointer.Int32(10),
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "busybox",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "busybox",
							Image: "busybox:latest",
						},
					},
				},
			},
		},
		Status: kruiseappsv1alpha1.DaemonSetStatus{
			CurrentNumberScheduled: 5,
			NumberMisscheduled:     2,
			DesiredNumberScheduled: 10,
			NumberReady:            10,
			ObservedGeneration:     1,
			UpdatedNumberScheduled: 0,
			NumberAvailable:        10,
			CollisionCount:         pointer.Int32(1),
		},
	}

	releaseDemo = &v1alpha1.BatchRelease{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rollouts.kruise.io/v1alpha1",
			Kind:       "BatchRelease",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "release",
			Namespace: daemonKey.Namespace,
			UID:       uuid.NewUUID(),
		},
		Spec: v1alpha1.BatchReleaseSpec{
			ReleasePlan: v1alpha1.ReleasePlan{
				Batches: []v1alpha1.ReleaseBatch{
					{
						CanaryReplicas: intstr.FromString("10%"),
					},
					{
						CanaryReplicas: intstr.FromString("50%"),
					},
					{
						CanaryReplicas: intstr.FromString("100%"),
					},
				},
			},
			TargetRef: v1alpha1.ObjectRef{
				WorkloadRef: &v1alpha1.WorkloadRef{
					APIVersion: daemonDemo.APIVersion,
					Kind:       daemonDemo.Kind,
					Name:       daemonDemo.Name,
				},
			},
		},
		Status: v1alpha1.BatchReleaseStatus{
			CanaryStatus: v1alpha1.BatchReleaseCanaryStatus{
				CurrentBatch: 0,
			},
		},
	}
)

func init() {
	apps.AddToScheme(scheme)
	v1alpha1.AddToScheme(scheme)
	kruiseappsv1alpha1.AddToScheme(scheme)
}

func TestCalculateBatchContext(t *testing.T) {
	RegisterFailHandler(Fail)

	percent := intstr.FromString("20%")
	cases := map[string]struct {
		workload func() *kruiseappsv1alpha1.DaemonSet
		release  func() *v1alpha1.BatchRelease
		result   *batchcontext.BatchContext
	}{
		"without NoNeedUpdate": {
			workload: func() *kruiseappsv1alpha1.DaemonSet {
				return &kruiseappsv1alpha1.DaemonSet{
					Spec: kruiseappsv1alpha1.DaemonSetSpec{
						UpdateStrategy: kruiseappsv1alpha1.DaemonSetUpdateStrategy{
							RollingUpdate: &kruiseappsv1alpha1.RollingUpdateDaemonSet{
								Partition: pointer.Int32Ptr(10),
							},
						},
					},
					Status: kruiseappsv1alpha1.DaemonSetStatus{
						CurrentNumberScheduled: 10,
						NumberMisscheduled:     5,
						DesiredNumberScheduled: 5,
						NumberReady:            10,
						ObservedGeneration:     1,
					},
				}
			},
			release: func() *v1alpha1.BatchRelease {
				r := &v1alpha1.BatchRelease{
					Spec: v1alpha1.BatchReleaseSpec{
						ReleasePlan: v1alpha1.ReleasePlan{
							FailureThreshold: &percent,
							Batches: []v1alpha1.ReleaseBatch{
								{
									CanaryReplicas: percent,
								},
							},
						},
					},
					Status: v1alpha1.BatchReleaseStatus{
						CanaryStatus: v1alpha1.BatchReleaseCanaryStatus{
							CurrentBatch: 0,
						},
					},
				}
				return r
			},
			result: &batchcontext.BatchContext{
				FailureThreshold:       &percent,
				CurrentBatch:           0,
				Replicas:               10,
				UpdatedReplicas:        5,
				UpdatedReadyReplicas:   5,
				PlannedUpdatedReplicas: 2,
				DesiredUpdatedReplicas: 2,
				CurrentPartition:       intstr.FromString("100%"),
				DesiredPartition:       intstr.FromString("80%"),
			},
		},
		// "with NoNeedUpdate": {
		// 	workload: func() *kruiseappsv1alpha1.DaemonSet {
		// 		return &kruiseappsv1alpha1.DaemonSet{
		// 			Spec: kruiseappsv1alpha1.DaemonSetSpec{
		// 				UpdateStrategy: kruiseappsv1alpha1.RollingUpdate{
		// 					Partition: func() *intstr.IntOrString { p := intstr.FromString("100%"); return &p }(),
		// 				},
		// 			},
		// 			Status: kruiseappsv1alpha1.CloneSetStatus{
		// 				Replicas:             20,
		// 				UpdatedReplicas:      10,
		// 				UpdatedReadyReplicas: 10,
		// 				AvailableReplicas:    20,
		// 				ReadyReplicas:        20,
		// 			},
		// 		}
		// 	},
		// 	release: func() *v1alpha1.BatchRelease {
		// 		r := &v1alpha1.BatchRelease{
		// 			Spec: v1alpha1.BatchReleaseSpec{
		// 				ReleasePlan: v1alpha1.ReleasePlan{
		// 					FailureThreshold: &percent,
		// 					Batches: []v1alpha1.ReleaseBatch{
		// 						{
		// 							CanaryReplicas: percent,
		// 						},
		// 					},
		// 				},
		// 			},
		// 			Status: v1alpha1.BatchReleaseStatus{
		// 				CanaryStatus: v1alpha1.BatchReleaseCanaryStatus{
		// 					CurrentBatch:         0,
		// 					NoNeedUpdateReplicas: pointer.Int32(10),
		// 				},
		// 				UpdateRevision: "update-version",
		// 			},
		// 		}
		// 		return r
		// 	},
		// 	result: &batchcontext.BatchContext{
		// 		CurrentBatch:           0,
		// 		UpdateRevision:         "update-version",
		// 		Replicas:               20,
		// 		UpdatedReplicas:        10,
		// 		UpdatedReadyReplicas:   10,
		// 		NoNeedUpdatedReplicas:  pointer.Int32Ptr(10),
		// 		PlannedUpdatedReplicas: 4,
		// 		DesiredUpdatedReplicas: 12,
		// 		CurrentPartition:       intstr.FromString("100%"),
		// 		DesiredPartition:       intstr.FromString("40%"),
		// 		FailureThreshold:       &percent,
		// 		FilterFunc:             labelpatch.FilterPodsForUnorderedUpdate,
		// 	},
		// },
	}

	for name, cs := range cases {
		t.Run(name, func(t *testing.T) {
			control := realController{
				object:       cs.workload(),
				WorkloadInfo: util.ParseWorkload(cs.workload()),
			}
			got, err := control.CalculateBatchContext(cs.release())
			fmt.Println(got)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Log()).Should(Equal(cs.result.Log()))
		})
	}
}

func TestRealController(t *testing.T) {
	RegisterFailHandler(Fail)

	release := releaseDemo.DeepCopy()
	daemon := daemonDemo.DeepCopy()
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(release, daemon.DeepCopy()).Build()
	c := NewController(cli, daemonKey, daemon.GroupVersionKind()).(*realController)
	controller, err := c.BuildController()
	Expect(err).NotTo(HaveOccurred())

	err = controller.Initialize(release)
	Expect(err).NotTo(HaveOccurred())
	fetch := &kruiseappsv1alpha1.DaemonSet{}
	Expect(cli.Get(context.TODO(), daemonKey, fetch)).NotTo(HaveOccurred())
	Expect(fetch.Spec.UpdateStrategy.RollingUpdate.Paused).Should(BeFalse())
	Expect(fetch.Annotations[util.BatchReleaseControlAnnotation]).Should(Equal(getControlInfo(release)))
	c.object = fetch // mock

	for {
		batchContext, err := controller.CalculateBatchContext(release)
		Expect(err).NotTo(HaveOccurred())
		err = controller.UpgradeBatch(batchContext)
		fetch = &kruiseappsv1alpha1.DaemonSet{}
		// mock
		Expect(cli.Get(context.TODO(), daemonKey, fetch)).NotTo(HaveOccurred())
		c.object = fetch
		if err == nil {
			break
		}
	}
	fetch = &kruiseappsv1alpha1.DaemonSet{}
	Expect(cli.Get(context.TODO(), daemonKey, fetch)).NotTo(HaveOccurred())
	Expect(fetch.Spec.UpdateStrategy.RollingUpdate.Partition).Should(Equal(9))

	err = controller.Finalize(release)
	Expect(err).NotTo(HaveOccurred())
	fetch = &kruiseappsv1alpha1.DaemonSet{}
	Expect(cli.Get(context.TODO(), daemonKey, fetch)).NotTo(HaveOccurred())
	Expect(fetch.Annotations[util.BatchReleaseControlAnnotation]).Should(Equal(""))

	stableInfo := controller.GetWorkloadInfo()
	Expect(stableInfo).ShouldNot(BeNil())
	checkWorkloadInfo(stableInfo, daemon)
}

func checkWorkloadInfo(stableInfo *util.WorkloadInfo, daemon *kruiseappsv1alpha1.DaemonSet) {
	Expect(stableInfo.Replicas).Should(Equal(daemon.Status.DesiredNumberScheduled))
	Expect(stableInfo.Status.Replicas).Should(Equal(daemon.Status.DesiredNumberScheduled))
	Expect(stableInfo.Status.ReadyReplicas).Should(Equal(daemon.Status.NumberReady))
	Expect(stableInfo.Status.UpdatedReplicas).Should(Equal(daemon.Status.UpdatedNumberScheduled))
	// how to compare here
	//Expect(stableInfo.Status.UpdatedReadyReplicas).Should(Equal(daemon.Status.UpdatedReadyReplicas))
	Expect(stableInfo.Status.UpdateRevision).Should(Equal(daemon.Status.DaemonSetHash))
	//Expect(stableInfo.Status.StableRevision).Should(Equal(daemon.Status.CurrentRevision))
	Expect(stableInfo.Status.AvailableReplicas).Should(Equal(daemon.Status.NumberAvailable))
	Expect(stableInfo.Status.ObservedGeneration).Should(Equal(daemon.Status.ObservedGeneration))
}

func getControlInfo(release *v1alpha1.BatchRelease) string {
	owner, _ := json.Marshal(metav1.NewControllerRef(release, release.GetObjectKind().GroupVersionKind()))
	return string(owner)
}
