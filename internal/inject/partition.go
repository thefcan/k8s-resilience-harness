package inject

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// DependencyPartitioner severs the workload from a stateful dependency by scaling
// that dependency's StatefulSet to zero for the fault window, then restoring its
// original replica count on rollback.
//
// It models a datastore partition: with the dependency gone, the requests that
// need it start failing, which the steady-state hypothesis is meant to catch.
// Unlike the other injectors (which the system is expected to ride out), this one
// is designed to violate steady state — it exercises the harness's *failure* path:
// a FAIL verdict and a non-zero exit.
type DependencyPartitioner struct {
	client     kubernetes.Interface
	namespace  string
	dependency string // StatefulSet to scale down, e.g. "redis"

	original *int32 // replica count captured by Inject, restored by Rollback
}

// NewDependencyPartitioner constructs a DependencyPartitioner that takes the named
// StatefulSet down for the duration of the fault.
func NewDependencyPartitioner(client kubernetes.Interface, namespace, dependency string) *DependencyPartitioner {
	return &DependencyPartitioner{client: client, namespace: namespace, dependency: dependency}
}

// Inject records the dependency's current replica count and scales it to zero,
// partitioning the workload from it. It returns the dependency name as the
// affected resource.
func (d *DependencyPartitioner) Inject(ctx context.Context) ([]string, error) {
	sts, err := d.client.AppsV1().StatefulSets(d.namespace).Get(ctx, d.dependency, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get statefulset %s/%s: %w", d.namespace, d.dependency, err)
	}

	current := int32(1)
	if sts.Spec.Replicas != nil {
		current = *sts.Spec.Replicas
	}
	if err := d.scale(ctx, 0); err != nil {
		return nil, fmt.Errorf("scale statefulset %s to 0: %w", d.dependency, err)
	}
	// Record the original count only once the scale-down has actually applied, so
	// Rollback never restores a count the cluster never saw.
	d.original = &current
	return []string{d.dependency}, nil
}

// Rollback restores the dependency to its original replica count.
func (d *DependencyPartitioner) Rollback(ctx context.Context) error {
	if d.original == nil {
		return nil
	}
	if err := d.scale(ctx, *d.original); err != nil {
		return fmt.Errorf("restore statefulset %s to %d replicas: %w", d.dependency, *d.original, err)
	}
	return nil
}

// scale patches the dependency's replica count. A strategic-merge patch touches
// only spec.replicas, so it can't conflict with the StatefulSet controller's
// concurrent status updates the way a read-modify-write Update would.
func (d *DependencyPartitioner) scale(ctx context.Context, replicas int32) error {
	patch := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas))
	_, err := d.client.AppsV1().StatefulSets(d.namespace).Patch(
		ctx, d.dependency, types.StrategicMergePatchType, patch, metav1.PatchOptions{},
	)
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("statefulset %s/%s not found", d.namespace, d.dependency)
	}
	return err
}
