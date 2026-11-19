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
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	scanv1 "github.com/Stilyan-Kirov/my-operator/api/v1"
	// import the API types for the ImageScan custom resource
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

const scanJobNamespace = "imagescan-project-system"

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
	// fetches the ImageScan object identified by req.NamespacedName from the
	// Fetch the cluster-scoped ImageScan by name and populate scan.
	err := r.Get(ctx, req.NamespacedName, scan)
	// Handle a failed read before using scan.
	if err != nil { // Indicating that the Get operation failed.
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get ImageScan %q: %w", req.Name, err)
	}

	/* -------------------------------------------------------- */
	//			Initialization of ImageScan					    //
	// Discover images and save their initial status only once. */
	/* ++++++++++++++++++++++++++++++++++++++++++++++++++++++++ */
	if scan.Status.Phase == "" {

		namespaceSelector, err := metav1.LabelSelectorAsSelector(&scan.Spec.NamespaceSelector) // Convert the Namespace selector into a filter usable by List.
		if err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"invalid namespace selector for ImageScan %q: %w", scan.Name, err,
			)
		}

		namespaces := &corev1.NamespaceList{}

		// Define the filter for Namespace labels.
		namespaceFilter := client.MatchingLabelsSelector{Selector: namespaceSelector} // Define the filter for Namespace labels.

		// Read matching Namespaces into namespaces.Items.
		err = r.List(ctx, namespaces, namespaceFilter) // List all Namespaces that match the label selector defined in the ImageScan spec. The results are stored in the namespaces variable.
		if err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"list Namespaces for ImageScan %q: %w", scan.Name, err,
			)
		}

		/* Creates selector from the label selector given in spec.podSelector*/
		podSelector, err := metav1.LabelSelectorAsSelector(&scan.Spec.PodSelector)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("invalid pod selector for ImageScan %q: %w", scan.Name, err)
		}

		/* Creates a filter for selecting Pods by label which is defined in spec.podSelector */
		podFilter := client.MatchingLabelsSelector{Selector: podSelector}

		// Keep one entry per image across all selected Namespaces and Pods.
		images := make(map[string]bool)
		// Visit every namespace.
		for _, namespace := range namespaces.Items {
			pods := &corev1.PodList{}

			// Restrict this request to the current Namespace.
			namespaceScope := client.InNamespace(namespace.Name)

			// Combines namespaceScope and podFilter to get a list of Pods in the given Namespace that match the podSelector. The results are stored in pods.
			err = r.List(ctx, pods, namespaceScope, podFilter)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf(
					"list Pods in Namespace %q: %w", namespace.Name, err,
				)
			}
			// Collect images from every matching Pod.
			for _, pod := range pods.Items {
				for _, container := range pod.Spec.Containers { // Итерира през всички контейнери в Pod и добавя техните образи в images map.
					images[container.Image] = true // String е образа , а bool е просто флаг за уникалност, не се използва за нищо друго.
				}
				for _, container := range pod.Spec.InitContainers {
					images[container.Image] = true
				}

				for _, container := range pod.Spec.EphemeralContainers {
					images[container.Image] = true
				}

			}
		}
		/* -------------------------------------------------------- */
		// 			Fill in found images to the images map          //
		/* ++++++++++++++++++++++++++++++++++++++++++++++++++++++++ */
		scan.Status.Scans = []scanv1.ImageScanResult{}
		scanNumber := 1
		for image := range images {
			result := scanv1.ImageScanResult{
				ID:    fmt.Sprintf("%s-%s-%d", strings.Split(image, ":")[0], scan.UID, scanNumber),
				Image: image,
				Phase: scanv1.ScanPhasePending,
			}
			// Add image record to the list.
			scan.Status.Scans = append(scan.Status.Scans, result)
			scanNumber++
		}
		// Mark the overall ImageScan as waiting for processing.
		scan.Status.Phase = scanv1.ScanPhasePending

		// Update the status subresource of the ImageScan object in Kubernetes with the new status information,
		// including the list of images to be scanned and their initial pending phase.
		err = r.Status().Update(ctx, scan)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf(
				"update status for ImageScan %q: %w", scan.Name, err,
			)
		}

		// Log the number of unique images selected for scanning in this ImageScan object, along with its name.
		logf.FromContext(ctx).Info(
			"Selected images for ImageScan",
			"name", scan.Name,
			"count", len(images),
		)
	}
	/* ++++++++++++++++++++++++++++++++++++++++++++ */

	if err := r.reconcileJobs(ctx, scan); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil

}

func (r *ImageScanReconciler) reconcileJobs(ctx context.Context, scan *scanv1.ImageScan) error {
	/* ctx е контекстът, който се използва за комуникация с Kubernetes API сървъра.
	ImageScan обекта е вече инициализирания обект */

	// flag for status changes, if any
	statusChanged := false
	// flag for job creation errors, if any
	var jobCreateErr error
	/* -------------------------------------------------------- */
	// 		Iterate through the list of images to scan          //
	/* ++++++++++++++++++++++++++++++++++++++++++++++++++++++++ */
	for i := range scan.Status.Scans {
		result := &scan.Status.Scans[i] // Go auto-dereferences for field access ✅

		if result.Phase == scanv1.ScanPhaseCompleted || result.Phase == scanv1.ScanPhaseFailed {
			continue
		}

		jobName := "imagescan-" + result.ID
		job := &batchv1.Job{}

		// Search for a Job object created by Trivy for scanning the image.
		err := r.Get(ctx, client.ObjectKey{Name: jobName, Namespace: scanJobNamespace}, job)
		// IF FOUND
		if err == nil {
			if result.Message != "" {
				result.Message = ""
				statusChanged = true
			}
			// Ако такъв Job съществува
			for _, condition := range job.Status.Conditions {
				if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
					result.Phase = scanv1.ScanPhaseCompleted
					statusChanged = true
					break
				}
				if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
					result.Phase = scanv1.ScanPhaseFailed
					result.Message = condition.Reason
					statusChanged = true
					break
				}
			}
			if result.Phase == scanv1.ScanPhasePending {
				result.Phase = scanv1.ScanPhaseRunning
				statusChanged = true
			}
			// Pass to the next image scan result, as this one is already being processed.
			continue
		}
		if !apierrors.IsNotFound(err) { // If the error is not a "not found" error
			return fmt.Errorf("get Job %q: %w", jobName, err)
		}

		/* -------------------------------- */
		// 		CREATE NEW Trivy Job       //
		/* ------------------------------- */
		newJob := jobForResult(*result, scan.Spec.Severity)
		/* Set ImageScan as the Job's controller owner.
			This enables garbage collection when ImageScan is deleted
		 	and lets the Job watch map events back to ImageScan.*/
		err = controllerutil.SetControllerReference(scan, newJob, r.Scheme)
		if err != nil {
			return fmt.Errorf("set ImageScan owner on Job %q: %w", newJob.Name, err)
		}

		// Create the Job in Kubernetes, which will start a Trivy container to scan the image.
		err = r.Create(ctx, newJob)

		if err != nil {
			jobCreateErr = fmt.Errorf("create Job %q: %w", newJob.Name, err)

			if result.Message != jobCreateErr.Error() {
				result.Message = jobCreateErr.Error()
				statusChanged = true
			}

			break
		}
	}
	/* --------------------------------*/
	// 		Set overall status         //
	overallPhase := overallScanPhase(scan.Status.Scans)

	if scan.Status.Phase != overallPhase {
		// адреса на обобщения статус на ImageScan се определя въз основа на индивидуалните резултати от сканирането.
		scan.Status.Phase = overallPhase
		statusChanged = true
	}

	// Update the status subresource
	if statusChanged {
		if err := r.Status().Update(ctx, scan); err != nil {
			return fmt.Errorf("update status for ImageScan %q: %w", scan.Name, err)
		}
	}
	/* Delete completed/failed jobs, while JSON reports are stored in PVC */
	for _, result := range scan.Status.Scans {

		// Check if the scan result is not completed or failed, and skip deletion of the Job in that case.
		if result.Phase != scanv1.ScanPhaseCompleted && result.Phase != scanv1.ScanPhaseFailed {
			continue
		}

		job := &batchv1.Job{ // Delete the Job associated with the completed scan result with the same name and namespace as the Job created earlier.
			ObjectMeta: metav1.ObjectMeta{
				Name:      "imagescan-" + result.ID,
				Namespace: scanJobNamespace,
			},
		}

		// DELETE the Job for this finished scan; ignore NotFound if it is already deleted.
		if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil && !apierrors.IsNotFound(err) {
			// specify a deletion propagation policy to ensure that the Job's Pods are also deleted when the Job is deleted.
			return fmt.Errorf("delete Job %q: %w", job.Name, err)
		}
	}
	return jobCreateErr
}

func jobForResult(result scanv1.ImageScanResult, severity string) *batchv1.Job {
	// Include Trivy arguments to args
	args := []string{
		"image",
		"--format", "json",
		"--output", "/reports/" + result.ID + ".json",
	}

	// Add --severity only if present.
	if severity != "" {
		args = append(args, "--severity", severity)
	}

	// The image to scan is always the last argument.
	args = append(args, result.Image)

	/* result is the scan result for which to create a Job*/
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "imagescan-" + result.ID,
			Namespace: scanJobNamespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:  "trivy",
						Image: "aquasec/trivy:0.76.1",
						// Args:  []string{"image", "--format", "json", result.Image},
						Args: args,

						VolumeMounts: []corev1.VolumeMount{{ // Къде в контейнера ще се монтира PVC, за да се запазят резултатите от сканирането.
							Name:      "reports",
							MountPath: "/reports",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "reports",
						VolumeSource: corev1.VolumeSource{ // Връзка между PVC и Volume, който ще се монтира в контейнера.
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

func overallScanPhase(results []scanv1.ImageScanResult) scanv1.ScanPhase {
	allCompleted := len(results) > 0
	anyFailed := false
	anyRunning := false

	for _, result := range results {
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

	if anyFailed {
		return scanv1.ScanPhaseFailed
	}
	if allCompleted {
		return scanv1.ScanPhaseCompleted
	}
	if anyRunning {
		return scanv1.ScanPhaseRunning
	}
	return scanv1.ScanPhasePending
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImageScanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&scanv1.ImageScan{}).
		Owns(&batchv1.Job{}).
		Named("imagescan").
		Complete(r)
}
