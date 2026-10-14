package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/example/modeswitch/internal/modespec"
)

type WorkloadKind string

const (
	KindDeployment  WorkloadKind = "Deployment"
	KindStatefulSet WorkloadKind = "StatefulSet"
)

// WorkloadReconciler drives spec.replicas of one workload kind from the
// current mode and the workload's replicas annotation.
type WorkloadReconciler struct {
	client.Client
	Clientset kubernetes.Interface
	Kind      WorkloadKind
	State     *ModeState
	Known     map[string]bool
	Recorder  record.EventRecorder
}

func (r *WorkloadReconciler) newObject() client.Object {
	if r.Kind == KindStatefulSet {
		return &appsv1.StatefulSet{}
	}
	return &appsv1.Deployment{}
}

func replicasOf(obj client.Object) *int32 {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		return o.Spec.Replicas
	case *appsv1.StatefulSet:
		return o.Spec.Replicas
	}
	return nil
}

func (r *WorkloadReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	mode, ok := r.State.Current()
	if !ok {
		// No mode received yet (startup / new leader). Touch nothing.
		return ctrl.Result{}, nil
	}

	obj := r.newObject()
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !obj.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	ann := obj.GetAnnotations()
	raw, has := ann[AnnotationReplicas]
	if !has {
		r.Recorder.Eventf(obj, corev1.EventTypeWarning, "MissingAnnotation",
			"labelled %s=true but has no %s annotation; leaving replicas alone", LabelEnabled, AnnotationReplicas)
		return ctrl.Result{}, nil
	}
	spec, err := modespec.Parse(raw, r.Known)
	if err != nil {
		// Not retried: the next annotation edit or mode change re-triggers.
		r.Recorder.Eventf(obj, corev1.EventTypeWarning, "InvalidAnnotation", "%s: %v", AnnotationReplicas, err)
		return ctrl.Result{}, nil
	}

	desired := spec.ReplicasFor(mode)
	current := replicasOf(obj)
	cur := int32(1) // API default; replicas is always set on live objects in practice
	if current != nil {
		cur = *current
	}

	// Default: apply the mode immediately.
	target, appliedMode, pendingSince := desired, mode, ""
	var requeue time.Duration

	// Delayed scale-to-zero: hold current replicas and record when the hold
	// started. Leaving a zero mode before expiry clears the pending marker
	// (it's simply omitted from the next apply), cancelling the countdown.
	if desired == 0 && cur > 0 {
		delay, err := parseDelay(ann[AnnotationScaleToZeroDelay])
		if err != nil {
			r.Recorder.Eventf(obj, corev1.EventTypeWarning, "InvalidAnnotation", "%s: %v", AnnotationScaleToZeroDelay, err)
			return ctrl.Result{}, nil
		}
		if delay > 0 {
			now := time.Now().UTC()
			since, err := time.Parse(time.RFC3339, ann[AnnotationZeroPendingSince])
			if err != nil || since.After(now) {
				since = now // absent, malformed, or clock-skewed: start the countdown now
			}
			if remaining := delay - now.Sub(since); remaining > 0 {
				target = cur
				appliedMode = ann[AnnotationAppliedMode] // still running the previous mode's count
				pendingSince = since.Format(time.RFC3339)
				requeue = remaining
			}
		}
	}

	if cur == target && ann[AnnotationAppliedMode] == appliedMode && ann[AnnotationZeroPendingSince] == pendingSince {
		return ctrl.Result{RequeueAfter: requeue}, nil
	}

	annotations := map[string]string{}
	if appliedMode != "" {
		annotations[AnnotationAppliedMode] = appliedMode
	}
	if pendingSince != "" {
		annotations[AnnotationZeroPendingSince] = pendingSince
	}
	if err := r.apply(ctx, obj, target, annotations); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply replicas: %w", err)
	}

	switch {
	case pendingSince != "" && ann[AnnotationZeroPendingSince] == "":
		logger.Info("scale to zero pending", "mode", mode, "replicas", cur, "in", requeue.Round(time.Second))
		r.Recorder.Eventf(obj, corev1.EventTypeNormal, "ScaleToZeroPending",
			"mode %q: holding %d replicas, scaling to 0 in %s", mode, cur, requeue.Round(time.Second))
	case pendingSince == "" && ann[AnnotationZeroPendingSince] != "" && target != 0:
		logger.Info("scale to zero cancelled", "mode", mode, "to", target)
		r.Recorder.Eventf(obj, corev1.EventTypeNormal, "ScaleToZeroCancelled", "mode %q: replicas %d -> %d", mode, cur, target)
	case cur != target:
		logger.Info("applied mode", "mode", mode, "from", cur, "to", target)
		r.Recorder.Eventf(obj, corev1.EventTypeNormal, "ModeApplied", "mode %q: replicas %d -> %d", mode, cur, target)
	}
	return ctrl.Result{RequeueAfter: requeue}, nil
}

func parseDelay(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("must not be negative")
	}
	return d, nil
}

// apply uses server-side apply with a dedicated field manager, so ownership of
// spec.replicas and the controller's annotations is recorded in managedFields.
// ArgoCD keys its ignoreDifferences off that manager name. Any controller
// annotation omitted from ann is removed by SSA (that's how pending clears).
func (r *WorkloadReconciler) apply(ctx context.Context, obj client.Object, n int32, ann map[string]string) error {
	opts := metav1.ApplyOptions{FieldManager: FieldManager, Force: true}
	ns, name := obj.GetNamespace(), obj.GetName()

	switch r.Kind {
	case KindDeployment:
		ac := appsv1ac.Deployment(name, ns).
			WithAnnotations(ann).
			WithSpec(appsv1ac.DeploymentSpec().WithReplicas(n))
		_, err := r.Clientset.AppsV1().Deployments(ns).Apply(ctx, ac, opts)
		return err
	case KindStatefulSet:
		ac := appsv1ac.StatefulSet(name, ns).
			WithAnnotations(ann).
			WithSpec(appsv1ac.StatefulSetSpec().WithReplicas(n))
		_, err := r.Clientset.AppsV1().StatefulSets(ns).Apply(ctx, ac, opts)
		return err
	}
	return fmt.Errorf("unsupported kind %q", r.Kind)
}

// SetupWithManager watches the workload kind (spec/annotation changes, so
// drift is corrected) plus a channel fed by the Broadcaster on mode change.
func (r *WorkloadReconciler) SetupWithManager(mgr ctrl.Manager, modeEvents <-chan event.GenericEvent) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("modeswitch-" + strings.ToLower(string(r.Kind))).
		For(r.newObject(), builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		))).
		WatchesRawSource(source.Channel(modeEvents, &handler.EnqueueRequestForObject{})).
		WithOptions(controller.Options{MaxConcurrentReconciles: 4}).
		Complete(r)
}
