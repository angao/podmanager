/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package podmanager

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/angao/podmanager/pkg/apis"
	extensionsv1alpha1 "github.com/angao/podmanager/pkg/apis/extensions/v1alpha1"
	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var c client.Client

var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: "foo", Namespace: "default"}}
var depKey = types.NamespacedName{Name: "website", Namespace: "default"}
var repKey = types.NamespacedName{Name: "website-123q", Namespace: "default"}

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = appsv1.SchemeGroupVersion.WithKind("Deployment")

const timeout = time.Second * 5

var cfg *rest.Config

func TestMain(m *testing.M) {
	t := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "config", "crds")},
	}
	apis.AddToScheme(scheme.Scheme)

	var err error
	if cfg, err = t.Start(); err != nil {
		log.Fatal(err)
	}

	code := m.Run()
	t.Stop()
	os.Exit(code)
}

// SetupTestReconcile returns a reconcile.Reconcile implementation that delegates to inner and
// writes the request to requests after Reconcile is finished.
func SetupTestReconcile(inner reconcile.Reconciler) (reconcile.Reconciler, chan reconcile.Request) {
	requests := make(chan reconcile.Request)
	fn := reconcile.Func(func(req reconcile.Request) (reconcile.Result, error) {
		result, err := inner.Reconcile(req)
		requests <- req
		return result, err
	})
	return fn, requests
}

// StartTestManager adds recFn
func StartTestManager(mgr manager.Manager, g *gomega.GomegaWithT) chan struct{} {
	stop := make(chan struct{})
	go func() {
		g.Expect(mgr.Start(stop)).NotTo(gomega.HaveOccurred())
	}()
	return stop
}

func newPodManager(t extensionsv1alpha1.PodManagerStrategy) *extensionsv1alpha1.PodManager {
	instance := &extensionsv1alpha1.PodManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: extensionsv1alpha1.PodManagerSpec{
			DeploymentName: "website",
			IPSet: extensionsv1alpha1.IPSet{
				"172.17.0.15",
				"172.17.0.16",
			},
			Resources: &extensionsv1alpha1.UpdatedContainerResources{
				Containers: []extensionsv1alpha1.ContainerResource{
					{
						ContainerName: "front",
						Image:         "nginx",
						Resources:     nil,
					},
				},
			},
			Strategy: t,
		},
	}
	return instance
}

func newDeployment(replicas int) *appsv1.Deployment {
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "website",
			Namespace: "default",
			UID:       uuid.NewUUID(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: func() *int32 { i := int32(replicas); return &i }(),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"type": "production"}},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"name": "foo",
						"type": "production",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:            "front",
							Image:           "redis",
							ImagePullPolicy: v1.PullIfNotPresent,
						},
					},
					RestartPolicy: v1.RestartPolicyAlways,
				},
			},
		},
	}
	return d
}

func newReplicaSet(d *appsv1.Deployment, name string, replicas int) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			UID:             uuid.NewUUID(),
			Namespace:       metav1.NamespaceDefault,
			Labels:          d.Spec.Selector.MatchLabels,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(d, controllerKind)},
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: d.Spec.Selector,
			Replicas: func() *int32 { i := int32(replicas); return &i }(),
			Template: d.Spec.Template,
		},
	}
}

func newPod(d *appsv1.ReplicaSet, podName string) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            podName,
			Labels:          d.Spec.Selector.MatchLabels,
			Namespace:       metav1.NamespaceDefault,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(d, appsv1.SchemeGroupVersion.WithKind("ReplicaSet"))},
		},
		Spec: d.Spec.Template.Spec,
	}
	return pod
}

func TestReconcile(t *testing.T) {
	replicas := 20
	g := gomega.NewGomegaWithT(t)

	instance := newPodManager(extensionsv1alpha1.PodManagerStrategy{Type: extensionsv1alpha1.PodDeleteStrategyType})
	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())
	defer close(StartTestManager(mgr, g))

	deploy := newDeployment(replicas)

	err = c.Create(context.TODO(), deploy)
	if apierrors.IsInvalid(err) {
		t.Logf("failed to create object, got an invalid object error: %v", err)
		return
	}
	g.Eventually(func() error { return c.Get(context.TODO(), depKey, deploy) }, timeout).Should(gomega.Succeed())

	replicaset := newReplicaSet(deploy, deploy.Name+"-123q", replicas)
	err = c.Create(context.TODO(), replicaset)
	if apierrors.IsInvalid(err) {
		t.Logf("failed to create object, got an invalid object error: %v", err)
		return
	}
	g.Eventually(func() error { return c.Get(context.TODO(), repKey, replicaset) }, timeout).Should(gomega.Succeed())

	for i := 0; i < replicas; i++ {
		podName := replicaset.Name + "-" + fmt.Sprintf("%d", i+1)
		pod := newPod(replicaset, podName)
		err = c.Create(context.TODO(), pod)
		if apierrors.IsInvalid(err) {
			t.Logf("failed to create object, got an invalid object error: %v", err)
			return
		}
	}

	// Create the PodManager object and expect the Reconcile and Deployment to be created
	err = c.Create(context.TODO(), instance)
	// The instance object may not be a valid object because it might be missing some required fields.
	// Please modify the instance object by adding required fields and then remove the following if statement.
	if apierrors.IsInvalid(err) {
		t.Logf("failed to create object, got an invalid object error: %v", err)
		return
	}
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), instance)
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

}
