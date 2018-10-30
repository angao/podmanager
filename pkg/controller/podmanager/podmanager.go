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
	"context"
	"fmt"
	"sort"
	"time"

	extensionsv1alpha1 "github.com/angao/podmanager/pkg/apis/extensions/v1alpha1"
	"github.com/angao/podmanager/pkg/util"
	"github.com/golang/glog"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	// GrayscaleAnnotation specify the deployment need to grayscale.
	GrayscaleAnnotation = "app.sncloud.com/grayscale"

	// ScaleDownAnnotation specify the pod need to delete.
	ScaleDownAnnotation = "app.sncloud.com/scaledown"
)

// PodManagerID specify unique PodManager
type PodManagerID struct {
	Name, Namespace string
}

// Add creates a new PodManager Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcilePodManager{
		Client:      mgr.GetClient(),
		scheme:      mgr.GetScheme(),
		PodManagers: make(map[PodManagerID]*time.Timer),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("podmanager-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to PodManager
	err = c.Watch(&source.Kind{Type: &extensionsv1alpha1.PodManager{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcilePodManager{}

// ReconcilePodManager reconciles a PodManager object
type ReconcilePodManager struct {
	client.Client
	scheme      *runtime.Scheme
	PodManagers map[PodManagerID]*time.Timer
}

// Reconcile reads that state of the cluster for a PodManager object and makes changes based on the state read
// and what is in the PodManager.Spec
//
// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=extensions.sncloud.com,resources=podmanagers,verbs=get;list;watch;create;update;patch;delete
func (r *ReconcilePodManager) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the PodManager instance
	glog.V(4).Infof("Fetch the PodManager: %s", request.NamespacedName)

	start := time.Now()
	defer func() {
		glog.Infof("One reconcile end: %s", time.Since(start).String())
	}()

	podManager := &extensionsv1alpha1.PodManager{}
	err := r.Get(context.TODO(), request.NamespacedName, podManager)
	pmID := PodManagerID{Name: request.Name, Namespace: request.Namespace}
	if err != nil {
		if errors.IsNotFound(err) {
			if timer, ok := r.PodManagers[pmID]; ok {
				delete(r.PodManagers, pmID)
				timer.Stop()
			}
		}
		glog.Errorf("Get PodManager %s: %v", request.NamespacedName, err)
		return reconcile.Result{}, err
	}

	// only if the deploymentUpdated is false, then keep on updating
	if podManager.Status.DeploymentUpdated {
		glog.Warning("podManager `updated` need to be false")
		return reconcile.Result{}, fmt.Errorf("podManager `updated` need to be false")
	}

	_, err = util.ValidatePodManager(podManager)
	if err != nil {
		glog.Errorf("podmanager validation: %+v", err)
		r.updatePodManagerCondition(podManager, err.Error(), "ValidatePodManager")
		return reconcile.Result{}, err
	}

	glog.Info("Start to Update.")

	deployment := &appsv1.Deployment{}
	err = r.Get(context.TODO(), types.NamespacedName{Name: podManager.Spec.DeploymentName, Namespace: podManager.Namespace}, deployment)
	if err != nil {
		glog.Errorf("Get deployment: %v", err)
		r.updatePodManagerCondition(podManager, err.Error(), "GetDeployment")
		return reconcile.Result{}, err
	}

	// List ReplicaSets owned by this Deployment, while reconciling ControllerRef through adoption/orphaning.
	rsList, err := r.getReplicaSetsForDeployment(deployment)
	if err != nil {
		glog.Errorf("List ReplicaSets for Deployment: %v", err)
		return reconcile.Result{}, err
	}

	sort.Sort(util.ReplicaSetsByCreationTimestamp(rsList))

	// List all Pods owned by this Deployment, grouped by their ReplicaSet.
	podMap, err := r.getPodMapForDeployment(deployment, rsList)
	if err != nil {
		glog.Errorf("List Pods for Deployment: %v", err)
		return reconcile.Result{}, err
	}

	ipSet := podManager.Spec.IPSet
	ipMap := make(map[string]interface{})
	for _, ip := range ipSet {
		ipMap[ip] = nil
	}

	// podUpgradeCount tells the deployment that there are several pods that need to be delete or updated
	podUpgradeCount, err := r.updatePodForDeployment(podManager, podMap, ipMap)
	if err != nil {
		glog.Errorf("Update Pod for Deployment: %v", err)
		r.updatePodManagerCondition(podManager, err.Error(), "PodUpdate")
		return reconcile.Result{}, err
	}

	if podUpgradeCount == 0 {
		glog.Errorf("There is nothing to update")
		return reconcile.Result{}, fmt.Errorf("there is nothing to update")
	}

	glog.V(4).Infof("%d pods need to be deleted or upgraded", podUpgradeCount)

	switch podManager.Spec.Strategy.Type {
	case extensionsv1alpha1.PodUpgradeStrategyType:
		if len(podManager.Spec.ScaleTimestamp) > 0 {
			glog.V(4).Info("Enter a timer task.")

			if _, ok := r.PodManagers[pmID]; !ok {
				go r.delayExecute(pmID, podManager, deployment, podUpgradeCount)
			}
			return reconcile.Result{}, nil
		}
		err = r.updateDeployment(podManager, deployment, podUpgradeCount)
		if err != nil {
			glog.Errorf("Update podManager: %v", err)
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	case extensionsv1alpha1.PodDeleteStrategyType:
		var message, reason string
		if podManager.Spec.Strategy.Phase == extensionsv1alpha1.Deleting {
			// adjust deployment replicas and deployment controller will watch this change.
			*(deployment.Spec.Replicas) -= int32(podUpgradeCount)
			err := r.Update(context.TODO(), deployment)
			if err != nil {
				glog.Errorf("Update deployment %s/%s replicas: %v", deployment.Namespace, deployment.Name, err)
				return reconcile.Result{}, err
			}
			reason = "DeploymentUpdated"
			message = fmt.Sprintf("Adjust deployment \"%s/%s\" replicas.", deployment.Namespace, deployment.Name)
		} else {
			reason = "PodUpdated"
			message = fmt.Sprintf("Pod has already bind")
			podManager.Spec.Strategy.Phase = extensionsv1alpha1.Binding
		}

		// if deployment is updated, update podManager status
		err = r.updatePodManagerCondition(podManager, message, reason)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}
	return reconcile.Result{}, fmt.Errorf("unexpected podManager strategy type: %s", podManager.Spec.Strategy.Type)
}

func (r *ReconcilePodManager) delayExecute(pmID PodManagerID,
	podManager *extensionsv1alpha1.PodManager, deployment *appsv1.Deployment, podUpgradeCount int) {
	t, _ := time.ParseInLocation(util.Layout, podManager.Spec.ScaleTimestamp, time.Local)
	d := t.Sub(time.Now())

	f := func() {
		err := r.updateDeployment(podManager, deployment, podUpgradeCount)
		if err != nil {
			glog.Errorf("Time task failed: %v", err)
		}
		delete(r.PodManagers, pmID)
	}
	timer := time.AfterFunc(d, f)
	r.PodManagers[pmID] = timer
}

// updateDeployment use to update the image and resource of deployment
// according to the given container name and tag annotation on deployment.
// Tag annotation is used to specify which pod needs to be upgraded.
func (r *ReconcilePodManager) updateDeployment(podManager *extensionsv1alpha1.PodManager, deployment *appsv1.Deployment, podUpgradeCount int) error {
	// add annotation to deployment and update deployment container
	deployment.Annotations[GrayscaleAnnotation] = fmt.Sprintf("%d", podUpgradeCount)

	// update the container image, env and resource
	containers := deployment.Spec.Template.Spec.Containers
	for i := range containers {
		for _, resourceContainer := range podManager.Spec.Resources.Containers {
			if containers[i].Name != resourceContainer.ContainerName {
				continue
			}
			if len(resourceContainer.Image) > 0 {
				containers[i].Image = resourceContainer.Image
			}
			if len(resourceContainer.Env) != 0 {
				containers[i].Env = resourceContainer.Env
			}
			if resourceContainer.Resources != nil {
				containers[i].Resources = *resourceContainer.Resources
			}
		}
	}
	deployment.Spec.Template.Spec.Containers = containers

	err := r.Update(context.TODO(), deployment)
	if err != nil {
		return err
	}

	// if deployment is updated, update podManager status
	message := fmt.Sprintf("Deployment \"%s/%s\" is updated.", deployment.Namespace, deployment.Name)
	err = r.updatePodManagerCondition(podManager, message, "DeploymentUpdated")
	if err != nil {
		return err
	}
	return nil
}

// getReplicaSetsForDeployment uses ControllerRefManager to reconcile
// ControllerRef by adopting and orphaning.
// It returns the list of ReplicaSets that this Deployment should manage.
func (r *ReconcilePodManager) getReplicaSetsForDeployment(d *appsv1.Deployment) ([]*appsv1.ReplicaSet, error) {
	// List all ReplicaSets to find those we own but that no longer match our selector.
	rsList := &appsv1.ReplicaSetList{}
	deploymentSelector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("deployment %s/%s has invalid label selector: %v", d.Namespace, d.Name, err)
	}

	err = r.List(context.TODO(), &client.ListOptions{
		LabelSelector: deploymentSelector,
		Namespace:     d.Namespace,
	}, rsList)
	if err != nil {
		return nil, err
	}

	sets := make([]*appsv1.ReplicaSet, 0)
	for i := range rsList.Items {
		sets = append(sets, &rsList.Items[i])
	}

	canAdoptFunc := util.RecheckDeletionTimestamp(func() (metav1.Object, error) {
		fresh := &appsv1.Deployment{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: d.Name, Namespace: d.Namespace}, fresh)
		if err != nil {
			return nil, err
		}
		if fresh.UID != d.UID {
			return nil, fmt.Errorf("original Deployment %v/%v is gone: got uid %v, wanted %v", d.Namespace, d.Name, fresh.UID, d.UID)
		}
		return fresh, nil
	})
	cm := util.NewReplicaSetControllerRefManager(d, canAdoptFunc)
	return cm.ClaimReplicaSets(sets)
}

// getPodsForDeployment returns the Pods managed by a Deployment.
func (r *ReconcilePodManager) getPodMapForDeployment(d *appsv1.Deployment, rsList []*appsv1.ReplicaSet) (map[types.UID]*v1.PodList, error) {
	// Get all Pods that potentially belong to this Deployment.
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return nil, err
	}

	pods := &v1.PodList{}
	err = r.List(context.TODO(), &client.ListOptions{
		LabelSelector: selector,
		Namespace:     d.Namespace,
	}, pods)
	if err != nil {
		return nil, err
	}

	// Group Pods by their controller (if it's in rsList).
	podMap := make(map[types.UID]*v1.PodList, len(rsList))
	for _, rs := range rsList {
		podMap[rs.UID] = &v1.PodList{}
	}
	for _, pod := range pods.Items {
		// Do not ignore inactive Pods because Recreate Deployments need to verify that no
		// Pods from older versions are running before spinning up new Pods.
		controllerRef := metav1.GetControllerOf(&pod)
		if controllerRef == nil {
			continue
		}
		// Only append if we care about this UID.
		if podList, ok := podMap[controllerRef.UID]; ok {
			podList.Items = append(podList.Items, pod)
		}
	}
	return podMap, nil
}

// updatePodForDeployment tag annotation for pod via ipMap, if the podIP is not in ipMap, it's annotation need to be deleted.
func (r *ReconcilePodManager) updatePodForDeployment(podManager *extensionsv1alpha1.PodManager, podMap map[types.UID]*v1.PodList, ipMap map[string]interface{}) (int, error) {
	strategy := 0
	var latestUID types.UID
	// decide whether to continue upgrading
	// if strategy > 1, it is continue upgrading
	for uid, podList := range podMap {
		if len(podList.Items) > 0 {
			strategy++
			latestUID = uid
		}
	}

	if strategy > 1 && podManager.Spec.Strategy.Type == extensionsv1alpha1.PodUpgradeStrategyType {
		for i := range podMap[latestUID].Items {
			pod := &podMap[latestUID].Items[i]
			if _, ok := ipMap[pod.Status.PodIP]; ok {
				delete(ipMap, pod.Status.PodIP)
			}
		}
		podUpgradeCount, err := r.updatePod(podMap, ipMap)
		if err != nil {
			return 0, err
		}
		return podUpgradeCount + len(podMap[latestUID].Items), nil
	}
	return r.updatePod(podMap, ipMap)
}

func (r *ReconcilePodManager) updatePod(podMap map[types.UID]*v1.PodList, ipMap map[string]interface{}) (int, error) {
	podUpgradeCount := 0
	for _, podList := range podMap {
		for i := range podList.Items {
			pod := &podList.Items[i]
			if pod.GetAnnotations() == nil {
				pod.Annotations = make(map[string]string)
			}

			_, ipOk := ipMap[pod.Status.PodIP]
			_, annotationOk := pod.Annotations[ScaleDownAnnotation]

			if !ipOk && annotationOk {
				delete(pod.Annotations, ScaleDownAnnotation)
				err := r.Update(context.TODO(), pod)
				if err != nil {
					return 0, err
				}
			}
			if !ipOk {
				continue
			}
			if !annotationOk {
				pod.Annotations[ScaleDownAnnotation] = "true"
				err := r.Update(context.TODO(), pod)
				if err != nil {
					return 0, err
				}
			}
			podUpgradeCount++
		}
	}
	if podUpgradeCount != len(ipMap) {
		return 0, fmt.Errorf("an IP does not exist in Deployment")
	}
	return podUpgradeCount, nil
}

// updatePodManagerCondition update the condition of PodManager
func (r *ReconcilePodManager) updatePodManagerCondition(podManager *extensionsv1alpha1.PodManager, message string, reason string) error {
	conditions := podManager.Status.Conditions
	condition := extensionsv1alpha1.PodManagerCondition{
		LastUpdateTime: metav1.Time{
			Time: time.Now(),
		},
		Message: message,
		Reason:  reason,
	}
	conditions = append(conditions, condition)

	podManager.Status.Conditions = conditions
	podManager.Status.DeploymentUpdated = true

	return r.Update(context.TODO(), podManager)
}
