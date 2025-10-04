/*
Copyright 2025.

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

package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	crdv1 "devops.toolbox/controller/api/v1"
)

// PodTrackerReconciler reconciles a PodTracker object
type PodTrackerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=crd.devops.toolbox,resources=podtrackers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=crd.devops.toolbox,resources=podtrackers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=crd.devops.toolbox,resources=podtrackers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the PodTracker object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *PodTrackerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	logger.Info("pod tracker trigged")

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodTrackerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crdv1.PodTracker{}).
		Named("podtracker").
		Complete(r)
}

func (r *PodTrackerReconciler) HandlePodEvents(pod client.Object) []reconcile.Request {
	log.Log.V(1).Info(pod.GetNamespace())
	if pod.GetNamespace() != "default" {
		return []reconcile.Request{}
	}
	namespacedNames := types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      pod.GetName(),
	}
	var podObject corev1.Pod
	err := r.Get(context.Background(), namespacedNames, &podObject)
	if err != nil {
		return []reconcile.Request{}
	}
	if len(podObject.Annotations) == 0 {
		log.Log.V(1).Info("no annotaion set")
	} else if podObject.GetAnnotations()["exampleannotation"] == "crd.devops.toolbox" {
		log.Log.V(1).Info("found a manager", podObject.Name)
	} else {
		return []reconcile.Request{}
	}
	podObject.SetAnnotations(map[string]string{
		"exampleAnnotation": "crd.devops.toolbox",
	})
	if err = r.Update(context.TODO(), &podObject); err != nil {
		log.Log.V(1).Info("error occured in update action")
	}
	return []reconcile.Request{}
}
