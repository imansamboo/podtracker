package controller

import (
	crdv1 "devops.toolbox/controller/api/v1"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func buildRayService(rt crdv1.RayTracker) *rayv1.RayService {

	return &rayv1.RayService{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ray.io/v1",
			Kind:       "RayService",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name: rt.Spec.Name,
		},

		Spec: rayv1.RayServiceSpec{

			// ============================
			// serveConfigV2 (YAML block)
			// ============================
			ServeConfigV2: `
proxy_location: EveryNode
http_options:
  host: 0.0.0.0
  port: 8000
applications:
  - name: text_ml_app
    import_path: text_ml:app
    route_prefix: /summarize_translate
    deployments:
      - name: Translator
        num_replicas: 1
        ray_actor_options:
          num_cpus: 0.1
        user_config:
          language: french
      - name: Summarizer
        num_replicas: 1
        ray_actor_options:
          num_cpus: 0.1
`,

			// ============================
			// RayClusterSpec
			// ============================
			RayClusterSpec: rayv1.RayClusterSpec{

				// version from CR (rt.Spec.Versioning or Image tag)
				RayVersion: rt.Spec.Versioning,

				// -------- HEAD GROUP --------
				HeadGroupSpec: rayv1.HeadGroupSpec{

					RayStartParams: map[string]string{
						"num-cpus": "0",
					},

					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "ray-head",
									Image: rt.Spec.Image, // from CR
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("1"),
											corev1.ResourceMemory: resource.MustParse("2Gi"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("1"),
											corev1.ResourceMemory: resource.MustParse("2Gi"),
										},
									},
								},
							},
						},
					},
				},

				// -------- WORKER GROUP --------
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						GroupName:   "small-group",
						Replicas:    pointer.Int32(2),
						MinReplicas: pointer.Int32(1),
						MaxReplicas: pointer.Int32(5),

						RayStartParams: map[string]string{},

						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "ray-worker",
										Image: rt.Spec.Image,
										Resources: corev1.ResourceRequirements{
											Requests: corev1.ResourceList{
												corev1.ResourceCPU:    resource.MustParse("500m"),
												corev1.ResourceMemory: resource.MustParse("2Gi"),
											},
											Limits: corev1.ResourceList{
												corev1.ResourceCPU:    resource.MustParse("1"),
												corev1.ResourceMemory: resource.MustParse("2Gi"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
