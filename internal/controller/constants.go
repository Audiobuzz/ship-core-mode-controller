package controller

const (
	Domain = "modeswitch.example.io"

	// LabelEnabled opts a Deployment/StatefulSet in. Used as the cache filter,
	// so the controller never even sees unlabelled workloads.
	LabelEnabled = Domain + "/enabled"

	// AnnotationReplicas holds the per-mode replica spec, e.g. "normal=3,peak=6".
	AnnotationReplicas = Domain + "/replicas"

	// AnnotationAppliedMode is written by the controller (same SSA field manager
	// as spec.replicas) to record which mode was last applied.
	AnnotationAppliedMode = Domain + "/applied-mode"

	// AnnotationScaleToZeroDelay (user-set, optional) is a Go duration, e.g.
	// "90s" or "5m". When the current mode wants 0 replicas, the workload is
	// held at its current count for this long first. Absent or "0" = immediate.
	AnnotationScaleToZeroDelay = Domain + "/scale-to-zero-delay"

	// AnnotationZeroPendingSince (controller-owned) is the RFC 3339 time the
	// pending scale-to-zero started. Persisted on the object so the countdown
	// survives controller restarts and leader failover.
	AnnotationZeroPendingSince = Domain + "/zero-pending-since"

	// FieldManager is the server-side-apply manager name. ArgoCD's
	// ignoreDifferences.managedFieldsManagers must reference this exact string.
	FieldManager = "modeswitch-controller"
)
