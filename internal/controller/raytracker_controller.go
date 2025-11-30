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
	"fmt"
	"time"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	crdv1 "devops.toolbox/controller/api/v1"
)

// RayTrackerReconciler reconciles a RayTracker object
type RayTrackerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=crd.devops.toolbox,resources=raytrackers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=crd.devops.toolbox,resources=raytrackers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=crd.devops.toolbox,resources=raytrackers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the RayTracker object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *RayTrackerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	_ = logf.FromContext(ctx)
	var rt crdv1.RayTracker
	if err := r.Get(ctx, req.NamespacedName, &rt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.Info("found new ray tracker", "name", rt.Name, "image", rt.Spec.Image+":"+rt.Spec.Versioning)

	// If deletion timestamp is set, let ownerReference cleanup handle RayService
	if !rt.DeletionTimestamp.IsZero() {
		// Nothing special here: we rely on OwnerReferences and garbage collection.
		log.Info("RayTracker is being deleted; skipping reconcile")
		_ = sendStatusToGin(rt.Spec.Token, "DELETED")
		return ctrl.Result{}, nil
	}

	// 2. Build the RayService object from RayTracker spec
	raySvc := buildRayService(rt)
	// ensure same namespace as RayTracker
	if rt.Namespace != "" {
		if raySvc.Namespace == "" {
			raySvc.Namespace = rt.Namespace
		}
	}

	// 3. Set owner reference so RayService is garbage-collected with RayTracker
	if err := ctrl.SetControllerReference(&rt, raySvc, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set owner reference: %w", err)
	}

	// 4. Apply (Server-Side Apply) the RayService
	if err := r.applyRayService(ctx, raySvc); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to apply RayService: %w", err)
	}
	err := sendStatusToGin(rt.Spec.Token, "CREATED")
	if err != nil {
		log.Error(err, "failed to send CREATED status to gin")
		// Do NOT return error — avoid infinite reconcile loop
	}

	// 5. Annotate RayTracker with last-applied time (safe alternative to status)
	if rt.Annotations == nil {
		rt.Annotations = map[string]string{}
	}
	rt.Annotations["raytracker.devops.toolbox/last-applied"] = time.Now().UTC().Format(time.RFC3339)
	if err := r.Update(ctx, &rt); err != nil {
		// Non-fatal: log and requeue
		log.Error(err, "failed to update RayTracker annotation")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	log.Info("RayService applied/updated successfully", "rayService", raySvc.Name)
	var raySvcLive rayv1.RayService
	err = r.Get(ctx, types.NamespacedName{
		Name: raySvc.Name, Namespace: raySvc.Namespace,
	}, &raySvcLive)
	if err == nil {
		sendStatusToGin(rt.Spec.Token, string(raySvcLive.Status.ServiceStatus))
	} else {
		log.Info("could not get sttaus of ray service", "rayService", raySvc.Name, "error", err.Error())
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayTrackerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crdv1.RayTracker{}).
		Owns(&rayv1.RayService{}).
		Named("raytracker").
		Complete(r)
}

func (r *RayTrackerReconciler) applyRayService(ctx context.Context, svc *rayv1.RayService) error {
	svc.SetGroupVersionKind(rayv1.SchemeGroupVersion.WithKind("RayService"))

	return r.Patch(ctx, svc, client.Apply, &client.PatchOptions{
		FieldManager: "raytracker-controller",
		Force:        pointer.Bool(true),
	})
}
