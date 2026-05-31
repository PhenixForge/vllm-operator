package controller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/apps/v1"

	infra "github.com/PhenixForge/vllm-operator/api/v1alpha1"
)

type VLLMModelReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infra.phenixforge.io,resources=vllmmodel,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infra.phenixforge.io,resources=vllmmodel/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infra.phenixforge.io,resources=vllmmodel/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

func (r *VLLMModelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	vllmModel := &infra.VLLMModel{}
	err := r.Get(ctx, req.NamespacedName, vllmModel)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get VLLMModel: %w", err)
	}

	if vllmModel.Status.Phase == "" {
		vllmModel.Status.Phase = "Provisioning"
		vllmModel.Status.Message = "Starting provisioning of resources"
		vllmModel.Status.LastUpdate = metav1.Now()
		if err := r.Status().Update(ctx, vllmModel); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	if err := r.reconcilePVC(ctx, vllmModel); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile PVC: %w", err)
	}

	if err := r.reconcileWarmupJob(ctx, vllmModel); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile warmup job: %w", err)
	}

	if err := r.reconcileDeployment(ctx, vllmModel); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile deployment: %w", err)
	}

	if err := r.updateStatus(ctx, vllmModel); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *VLLMModelReconciler) reconcilePVC(ctx context.Context, vllmModel *infra.VLLMModel) error {
	log := log.FromContext(ctx)
	pvc := &corev1.PersistentVolumeClaim{}
	pvcName := fmt.Sprintf("%s-pvc", vllmModel.Name)
	pvcKey := types.NamespacedName{Name: pvcName, Namespace: vllmModel.Namespace}

	if err := r.Get(ctx, pvcKey, pvc); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get PVC: %w", err)
		}

		pvc = &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: vllmModel.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "vllm-operator",
					"app.kubernetes.io/name":      vllmModel.Name,
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &vllmModel.Spec.StorageClass,
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(vllmModel.Spec.StorageSize),
					},
				},
			},
		}

		if err := ctrl.SetControllerReference(vllmModel, pvc, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference: %w", err)
		}

		if err := r.Create(ctx, pvc); err != nil {
			return fmt.Errorf("failed to create PVC: %w", err)
		}

		log.Info("Created PVC", "name", pvcName)
		vllmModel.Status.PVCName = pvcName
		vllmModel.Status.Phase = "Provisioning"
		vllmModel.Status.Message = "PVC created, waiting for bound status"
		vllmModel.Status.LastUpdate = metav1.Now()
		if err := r.Status().Update(ctx, vllmModel); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
	} else {
		if pvc.Status.Phase != corev1.VolumeBound {
			log.Info("PVC not yet bound", "name", pvcName, "phase", pvc.Status.Phase)
			vllmModel.Status.PVCName = pvcName
			vllmModel.Status.Phase = "Provisioning"
			vllmModel.Status.Message = fmt.Sprintf("PVC %s is %s", pvcName, pvc.Status.Phase)
			vllmModel.Status.LastUpdate = metav1.Now()
			if err := r.Status().Update(ctx, vllmModel); err != nil {
				return fmt.Errorf("failed to update status: %w", err)
			}
			return nil
		}
		vllmModel.Status.PVCName = pvcName
	}
	return nil
}

func (r *VLLMModelReconciler) reconcileWarmupJob(ctx context.Context, vllmModel *infra.VLLMModel) error {
	log := log.FromContext(ctx)
	job := &batchv1.Job{}
	jobName := fmt.Sprintf("%s-warmup-job", vllmModel.Name)
	jobKey := types.NamespacedName{Name: jobName, Namespace: vllmModel.Namespace}

	if err := r.Get(ctx, jobKey, job); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get Job: %w", err)
		}

		job = &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: vllmModel.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "vllm-operator",
					"app.kubernetes.io/name":      vllmModel.Name,
				},
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app.kubernetes.io/managed-by": "vllm-operator",
							"app.kubernetes.io/name":      vllmModel.Name,
						},
					},
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
						Containers: []corev1.Container{
							{
								Name:  "vllm-downloader",
								Image: "ghcr.io/vllm-project/vllm:latest",
								Command: []string{"python", "-m", "vllm.entrypoints.download"},
								Args: []string{
									"--model", vllmModel.Spec.ModelId,
									"--output-dir", "/data",
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "model-data",
										MountPath: "/data",
									},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "model-data",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: vllmModel.Status.PVCName,
									},
								},
							},
						},
					},
				},
			},
		}

		if err := ctrl.SetControllerReference(vllmModel, job, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference: %w", err)
		}

		if err := r.Create(ctx, job); err != nil {
			return fmt.Errorf("failed to create Job: %w", err)
		}

		log.Info("Created warmup Job", "name", jobName)
		vllmModel.Status.JobName = jobName
		vllmModel.Status.Phase = "Downloading"
		vllmModel.Status.Message = "Warmup Job created to download model weights"
		vllmModel.Status.LastUpdate = metav1.Now()
		if err := r.Status().Update(ctx, vllmModel); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
	} else {
		if job.Status.Succeeded > 0 {
			log.Info("Warmup Job completed", "name", jobName)
			vllmModel.Status.JobName = jobName
		} else if job.Status.Failed > 0 {
			log.Error(nil, "Warmup Job failed", "name", jobName)
			vllmModel.Status.Phase = "Failed"
			vllmModel.Status.Message = fmt.Sprintf("Warmup Job %s failed", jobName)
			vllmModel.Status.LastUpdate = metav1.Now()
			if err := r.Status().Update(ctx, vllmModel); err != nil {
				return fmt.Errorf("failed to update status: %w", err)
			}
			return fmt.Errorf("warmup job failed")
		} else {
			log.Info("Warmup Job in progress", "name", jobName, "status", job.Status)
			vllmModel.Status.JobName = jobName
			vllmModel.Status.Phase = "Downloading"
			vllmModel.Status.Message = fmt.Sprintf("Warmup Job %s in progress", jobName)
			vllmModel.Status.LastUpdate = metav1.Now()
			if err := r.Status().Update(ctx, vllmModel); err != nil {
				return fmt.Errorf("failed to update status: %w", err)
			}
			return nil
		}
	}
	return nil
}

func (r *VLLMModelReconciler) reconcileDeployment(ctx context.Context, vllmModel *infra.VLLMModel) error {
	log := log.FromContext(ctx)
	deployment := &v1.Deployment{}
	deploymentName := fmt.Sprintf("%s-deployment", vllmModel.Name)
	deploymentKey := types.NamespacedName{Name: deploymentName, Namespace: vllmModel.Namespace}

	if err := r.Get(ctx, deploymentKey, deployment); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get Deployment: %w", err)
		}

		deployment = &v1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deploymentName,
				Namespace: vllmModel.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "vllm-operator",
					"app.kubernetes.io/name":      vllmModel.Name,
				},
			},
			Spec: v1.DeploymentSpec{
				Replicas: &vllmModel.Spec.Replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app.kubernetes.io/managed-by": "vllm-operator",
						"app.kubernetes.io/name":      vllmModel.Name,
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app.kubernetes.io/managed-by": "vllm-operator",
							"app.kubernetes.io/name":      vllmModel.Name,
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "vllm-server",
								Image: "ghcr.io/vllm-project/vllm:latest",
								Command: []string{"python", "-m", "vllm.entrypoints.api_server"},
								Args: []string{
									"--model", "/data",
									"--host", "0.0.0.0",
									"--port", "8000",
								},
								Ports: []corev1.ContainerPort{
									{
										Name:          "http",
										ContainerPort: 8000,
									},
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "model-data",
										MountPath: "/data",
									},
								},
								Env:        convertEnvVars(vllmModel.Spec.Env),
								Resources: convertResources(vllmModel.Spec.Resources),
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "model-data",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: vllmModel.Status.PVCName,
									},
								},
							},
						},
					},
				},
			},
		}

		if err := ctrl.SetControllerReference(vllmModel, deployment, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference: %w", err)
		}

		if err := r.Create(ctx, deployment); err != nil {
			return fmt.Errorf("failed to create Deployment: %w", err)
		}

		log.Info("Created Deployment", "name", deploymentName)
		vllmModel.Status.DeploymentName = deploymentName
	} else {
		if *deployment.Spec.Replicas != vllmModel.Spec.Replicas {
			deployment.Spec.Replicas = &vllmModel.Spec.Replicas
			if err := r.Update(ctx, deployment); err != nil {
				return fmt.Errorf("failed to update Deployment: %w", err)
			}
			log.Info("Updated Deployment replicas", "name", deploymentName, "replicas", vllmModel.Spec.Replicas)
		}
		vllmModel.Status.DeploymentName = deploymentName
	}
	return nil
}

func (r *VLLMModelReconciler) updateStatus(ctx context.Context, vllmModel *infra.VLLMModel) error {
	log := log.FromContext(ctx)
	deployment := &v1.Deployment{}
	deploymentKey := types.NamespacedName{Name: vllmModel.Status.DeploymentName, Namespace: vllmModel.Namespace}

	if err := r.Get(ctx, deploymentKey, deployment); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get Deployment: %w", err)
	}

	readyPods := int32(0)
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == v1.DeploymentAvailable && condition.Status == corev1.ConditionTrue {
			readyPods = deployment.Status.AvailableReplicas
			break
		}
	}

	vllmModel.Status.PodsReady = readyPods
	vllmModel.Status.LastUpdate = metav1.Now()

	if readyPods == vllmModel.Spec.Replicas {
		vllmModel.Status.Phase = "Ready"
		vllmModel.Status.Message = fmt.Sprintf("Deployment %s is ready with %d replicas", vllmModel.Status.DeploymentName, readyPods)
	} else {
		vllmModel.Status.Phase = "Downloading"
		vllmModel.Status.Message = fmt.Sprintf("Waiting for Deployment %s to be ready (%d/%d pods)", vllmModel.Status.DeploymentName, readyPods, vllmModel.Spec.Replicas)
	}

	if err := r.Status().Update(ctx, vllmModel); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	log.Info("Updated status", "phase", vllmModel.Status.Phase, "readyPods", readyPods)
	return nil
}

func convertEnvVars(envVars []infra.EnvVar) []corev1.EnvVar {
	k8sEnvVars := make([]corev1.EnvVar, len(envVars))
	for i, env := range envVars {
		k8sEnvVars[i] = corev1.EnvVar{
			Name:  env.Name,
			Value: env.Value,
		}
	}
	return k8sEnvVars
}

func convertResources(resources infra.ResourceRequirements) corev1.ResourceRequirements {
	limits := corev1.ResourceList{}
	for k, v := range resources.Limits {
		limits[corev1.ResourceName(k)] = resource.MustParse(v)
	}
	requests := corev1.ResourceList{}
	for k, v := range resources.Requests {
		requests[corev1.ResourceName(k)] = resource.MustParse(v)
	}
	return corev1.ResourceRequirements{
		Limits:   limits,
		Requests: requests,
	}
}

func (r *VLLMModelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infra.VLLMModel{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&batchv1.Job{}).
		Owns(&v1.Deployment{}).
		Complete(r)
}