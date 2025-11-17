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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	crdv1 "devops.toolbox/controller/api/v1"
	appsv1 "k8s.io/api/apps/v1"
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
	log := ctrl.LoggerFrom(ctx)

	var tracker crdv1.PodTracker
	if err := r.Get(ctx, req.NamespacedName, &tracker); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	imageSpec := tracker.Spec.ImageSpec
	var image string
	if imageSpec.Repository != "" {
		image = fmt.Sprintf("%s/%s:%s", imageSpec.Repository, imageSpec.Image, imageSpec.Version)

	} else {
		image = fmt.Sprintf("%s:%s", imageSpec.Image, imageSpec.Version)

	}
	imagePath := strings.Split(imageSpec.Image, "/")
	var deployName string
	if len(imagePath) > 0 {
		deployName = imagePath[len(imagePath)-1]
	} else {
		return ctrl.Result{}, client.IgnoreNotFound(fmt.Errorf("no image found in your request"))
	}
	deployName = deployName + "-deployment"

	log.Info("Ensuring deployment exists", "image", image)

	// Build desired Deployment
	deploy := r.desiredDeployment(&tracker, deployName, image)

	var existing appsv1.Deployment
	err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: tracker.Namespace}, &existing)
	if apierrors.IsNotFound(err) {
		log.Info("Creating new deployment", "name", deployName)
		if err := r.Create(ctx, deploy); err != nil {
			return ctrl.Result{}, err
		}
	} else if err == nil {
		// Optional: compare spec and update if changed
		if !reflect.DeepEqual(existing.Spec.Template.Spec.Containers[0].Image, image) {
			existing.Spec.Template.Spec.Containers[0].Image = image
			if err := r.Update(ctx, &existing); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		return ctrl.Result{}, err
	}

	// Now monitor Deployment’s Pod status
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(tracker.Namespace), client.MatchingLabels{"app": deployName}); err != nil {
		return ctrl.Result{}, err
	}

	if len(pods.Items) == 0 {
		log.Info("No pods yet; requeueing")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	pod := pods.Items[0]
	if len(pod.Status.ContainerStatuses) == 0 {
		log.Info("No container status yet; requeueing")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	cs := pod.Status.ContainerStatuses[0]
	state := cs.State
	podImage := cs.Image
	imageID := cs.ImageID

	var status string
	var cond metav1.Condition

	if state.Waiting != nil {
		reason := state.Waiting.Reason
		switch reason {
		case "ErrImagePull", "ImagePullBackOff":
			status = "image_pull_failed"
			cond = metav1.Condition{
				Type:               "Degraded",
				Status:             metav1.ConditionTrue,
				Reason:             reason,
				Message:            fmt.Sprintf("Image pull failed for %s: %s", podImage, state.Waiting.Message),
				LastTransitionTime: metav1.Now(),
			}
		case "ContainerCreating":
			status = "image_pulling"
			cond = metav1.Condition{
				Type:               "Progressing",
				Status:             metav1.ConditionTrue,
				Reason:             reason,
				Message:            fmt.Sprintf("Image %s is being pulled", podImage),
				LastTransitionTime: metav1.Now(),
			}
		default:
			status = strings.ToLower(reason)
			cond = metav1.Condition{
				Type:               "Progressing",
				Status:             metav1.ConditionTrue,
				Reason:             reason,
				Message:            fmt.Sprintf("Container is waiting: %s", reason),
				LastTransitionTime: metav1.Now(),
			}
		}
	} else if state.Running != nil {
		status = "image_running"
		cond = metav1.Condition{
			Type:               "Available",
			Status:             metav1.ConditionTrue,
			Reason:             "ContainerRunning",
			Message:            fmt.Sprintf("Image %s is running (ID: %s)", podImage, imageID),
			LastTransitionTime: metav1.Now(),
		}
	} else if state.Terminated != nil {
		term := state.Terminated
		exitCode := term.ExitCode
		reason := term.Reason
		msg := term.Message
		started := term.StartedAt
		finished := term.FinishedAt

		// Default assumption: image pulled successfully, container exited
		status = "container_terminated"

		// Differentiate specific causes
		if reason == "Completed" && exitCode == 0 {
			status = "completed"
		} else if reason == "Error" || exitCode != 0 {
			status = "runtime_error"
		} else if strings.Contains(reason, "OOM") {
			status = "oom_killed"
		}

		cond = metav1.Condition{
			Type:   "Degraded",
			Status: metav1.ConditionTrue,
			Reason: reason,
			Message: fmt.Sprintf(
				"Container for image %s terminated (exitCode=%d, reason=%s, msg=%s, started=%s, finished=%s)",
				cs.Image, exitCode, reason, msg, started.Format(time.RFC3339), finished.Format(time.RFC3339),
			),
			LastTransitionTime: metav1.Now(),
		}

		log.Info("Container terminated",
			"image", cs.Image,
			"exitCode", exitCode,
			"reason", reason,
			"message", msg,
			"started", started,
			"finished", finished,
		)
	} else {
		status = "unknown"
		cond = metav1.Condition{
			Type:               "Unknown",
			Status:             metav1.ConditionUnknown,
			Reason:             "UnknownState",
			Message:            fmt.Sprintf("Unknown image state for %s", podImage),
			LastTransitionTime: metav1.Now(),
		}
	}

	log.Info("Image state", "image", podImage, "status", status, "imageID", imageID)

	if err := sendStatusToGin(tracker.Spec.ImageSpec.Token, status); err != nil {
		log.Error(err, "Failed to send status to Gin service")
	} else {
		log.Info(fmt.Sprintf("Pod status sent token=%s and status=%s", tracker.Spec.ImageSpec.Token, status))
	}

	// Update the condition (replace or append)
	meta.SetStatusCondition(&tracker.Status.Conditions, cond)

	// Update CRD status
	if err := r.Status().Update(ctx, &tracker); err != nil {
		log.Error(err, "Failed to update PodTracker status")
	}

	// Requeue while still progressing
	if cond.Type == "Progressing" {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	log.Info("Pod reached stable condition", "condition", cond.Type)
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
	} else if podObject.GetAnnotations()["exampleannotdISTyCHIgoSeation"] == "crd.devops.toolbox" {
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

func (r *PodTrackerReconciler) desiredDeployment(tracker *crdv1.PodTracker, name, image string) *appsv1.Deployment {
	labels := map[string]string{"app": name}

	replicas := int32(1)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tracker.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(tracker, crdv1.GroupVersion.WithKind("PodTracker")),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "tracked-container",
						Image: image,
					}},
				},
			},
		},
	}
}

type TokenState struct {
	Token  string `json:"token" example:"abcdef12345"`
	Status string `json:"status" example:"active"`
}

func sendStatusToGin(token, status string) error {
	url := "http://gin-service.default.svc.cluster.local:5000/update-state" // use k8s service name in-cluster
	url = "http://192.168.49.2:30056/update-state"
	payload := TokenState{
		Token:  token,
		Status: status,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update token, status code: %d", resp.StatusCode)
	}

	return nil
}
