/*
Copyright 2026.

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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	scanv1 "github.com/Stilyan-Kirov/my-operator/api/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ImageScanReconciler reconciles a ImageScan object
type ImageScanReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=scan.my.domain,resources=imagescans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=scan.my.domain,resources=imagescans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=scan.my.domain,resources=imagescans/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ImageScan object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.25.0/pkg/reconcile
func (r *ImageScanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	scan := &scanv1.ImageScan{} // Create an empty object that Get will populate.
	err := r.Get(ctx, req.NamespacedName, scan)
	// Handle a failed read before using scan.
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get ImageScan %q: %w", req.Name, err)
	}

	// Skip initialization if this ImageScan already has a saved status.
	// Discover images and save their initial status only once.
	if scan.Status.Phase == "" {
		//return ctrl.Result{}, nil

		namespaceSelector, err := metav1.LabelSelectorAsSelector(&scan.Spec.NamespaceSelector)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"invalid namespace selector for ImageScan %q: %w", scan.Name, err,
			)
		}

		namespaces := &corev1.NamespaceList{}

		// Define the filter for Namespace labels.
		namespaceFilter := client.MatchingLabelsSelector{Selector: namespaceSelector}

		// Read matching Namespaces into namespaces.Items.
		err = r.List(ctx, namespaces, namespaceFilter)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"list Namespaces for ImageScan %q: %w", scan.Name, err,
			)
		}
		// Convert the Pod selector into a filter usable by List.
		podSelector, err := metav1.LabelSelectorAsSelector(&scan.Spec.PodSelector)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("invalid pod selector for ImageScan %q: %w", scan.Name, err)
		}

		// Use the same label filter in every selected Namespace.
		podFilter := client.MatchingLabelsSelector{Selector: podSelector}

		// Keep one entry per image across all selected Namespaces and Pods.
		images := make(map[string]bool)
		// Visit every Namespace selected earlier.
		for _, namespace := range namespaces.Items {
			pods := &corev1.PodList{}

			// Restrict this request to the current Namespace.
			namespaceScope := client.InNamespace(namespace.Name)

			// Read Pods matching both the Namespace and the Pod label filter.
			err = r.List(ctx, pods, namespaceScope, podFilter)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf(
					"list Pods in Namespace %q: %w", namespace.Name, err,
				)
			}
			// Collect images from every matching Pod.
			for _, pod := range pods.Items {
				for _, container := range pod.Spec.Containers {
					images[container.Image] = true
				}

				/* KEEP THIS FOR LATER
				for _, container := range pod.Spec.InitContainers {
					images[container.Image] = true
				}

				for _, container := range pod.Spec.EphemeralContainers {
					images[container.Image] = true
				}
				*/
			}
		}

		// Start with an empty list of scan records.
		scan.Status.Scans = []scanv1.ImageScanResult{}

		// Number the images within this ImageScan.
		scanNumber := 1

		// Each key in the map is one unique image reference.
		for image := range images {
			// Prepare the status record for this image.
			result := scanv1.ImageScanResult{
				ID:    fmt.Sprintf("%s-%d", scan.UID, scanNumber),
				Image: image,
				Phase: scanv1.ScanPhasePending,
			}

			// Add the record to the list.
			scan.Status.Scans = append(scan.Status.Scans, result)

			// Use the next number for the next image.
			scanNumber++
		}
		// Mark the overall ImageScan as waiting for processing.
		scan.Status.Phase = scanv1.ScanPhasePending

		// Save the status through the Kubernetes status subresource.
		err = r.Status().Update(ctx, scan)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"update status for ImageScan %q: %w", scan.Name, err,
			)
		}

		logf.FromContext(ctx).Info(
			"Selected images for ImageScan",
			"name", scan.Name,
			"count", len(images),
		)
	}
	if err := r.reconcileJobs(ctx, scan); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil

}

func (r *ImageScanReconciler) reconcileJobs(ctx context.Context, scan *scanv1.ImageScan) error {
	//for _, result := range scan.Status.Scans {
	//	if result.Phase != scanv1.ScanPhasePending {
	//		continue
	//	}
	statusChanged := false
	for i := range scan.Status.Scans {
		result := &scan.Status.Scans[i]
		if result.Phase == scanv1.ScanPhaseCompleted || result.Phase == scanv1.ScanPhaseFailed {
			continue
		}

		jobName := "imagescan-" + result.ID
		job := &batchv1.Job{}

		err := r.Get(ctx, client.ObjectKey{Name: jobName, Namespace: "stilyan-kirov-system"}, job)
		if err == nil {
			for _, condition := range job.Status.Conditions {
				if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
					result.Phase = scanv1.ScanPhaseCompleted
					statusChanged = true
					break
				}
				if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
					result.Phase = scanv1.ScanPhaseFailed
					result.Message = condition.Message
					if result.Message == "" {
						result.Message = condition.Reason
					}
					statusChanged = true
					break
				}
			}
			if result.Phase == scanv1.ScanPhasePending {
				result.Phase = scanv1.ScanPhaseRunning
				statusChanged = true
			}
			continue
		}
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get Job %q: %w", jobName, err)
		}

		// Job Trivy created
		newJob := jobForResult(*result)
		err = controllerutil.SetControllerReference(scan, newJob, r.Scheme)
		if err != nil {
			return fmt.Errorf("set ImageScan owner on Job %q: %w", newJob.Name, err)
		}
		err = r.Create(ctx, newJob)

		if err != nil {
			return fmt.Errorf("create Job %q: %w", newJob.Name, err)
		}
	}

	allCompleted := len(scan.Status.Scans) > 0
	anyFailed := false
	anyRunning := false

	for _, result := range scan.Status.Scans {
		if result.Phase == scanv1.ScanPhaseFailed {
			anyFailed = true
		}
		if result.Phase == scanv1.ScanPhaseRunning {
			anyRunning = true
		}
		if result.Phase != scanv1.ScanPhaseCompleted {
			allCompleted = false
		}
	}

	overallPhase := scanv1.ScanPhasePending
	if anyFailed {
		overallPhase = scanv1.ScanPhaseFailed
	} else if allCompleted {
		overallPhase = scanv1.ScanPhaseCompleted
	} else if anyRunning {
		overallPhase = scanv1.ScanPhaseRunning
	}

	if scan.Status.Phase != overallPhase {
		scan.Status.Phase = overallPhase
		statusChanged = true
	}

	if statusChanged {
		if err := r.Status().Update(ctx, scan); err != nil {
			return fmt.Errorf("update status for ImageScan %q: %w", scan.Name, err)
		}
	}
	/*Delete completed jobs, why JSON reports are stored in PVC */
	for _, result := range scan.Status.Scans {
		if result.Phase != scanv1.ScanPhaseCompleted {
			continue
		}

		job := &batchv1.Job{ // Delete the Job associated with the completed scan result with the same name and namespace as the Job created earlier.
			ObjectMeta: metav1.ObjectMeta{
				Name:      "imagescan-" + result.ID,
				Namespace: "stilyan-kirov-system",
			},
		}
		if err := r.Delete(ctx, job); err != nil && !apierrors.IsNotFound(err) { // Ignore not found errors, as the Job may have already been deleted.
			return fmt.Errorf("delete Job %q: %w", job.Name, err)
		}
	}
	return nil
}

func jobForResult(result scanv1.ImageScanResult) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "imagescan-" + result.ID,
			Namespace: "stilyan-kirov-system",
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:  "trivy",
						Image: "aquasec/trivy:0.76.1",
						//Args:  []string{"image", "--format", "json", result.Image},
						Args: []string{
							"image",
							"--format", "json",
							"--output", "/reports/" + result.ID + ".json",
							result.Image,
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "reports",
							MountPath: "/reports",
						}},
					}}, // край на Containers
					Volumes: []corev1.Volume{{
						Name: "reports",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: "imagescan-results-pvc",
							},
						},
					}},
				}, // край на PodSpec
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImageScanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&scanv1.ImageScan{}).
		Owns(&batchv1.Job{}).
		Named("imagescan").
		Complete(r)
}
