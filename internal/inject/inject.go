// Package inject implements fault injectors for resilience experiments.
//
// Each injector depends only on kubernetes.Interface, so unit tests drive it
// with a fake clientset. M2 shipped pod-kill; M3 adds node-drain (cordon a node
// and evict the target workload from it). Network and resource-pressure faults
// follow.
package inject

import "context"

// Injector performs a single fault against the cluster and can undo whatever
// cluster-level state it changed.
type Injector interface {
	// Inject performs the fault and returns the names of the affected resources.
	Inject(ctx context.Context) (affected []string, err error)

	// Rollback reverses any standing cluster state the fault left behind — e.g.
	// uncordon a drained node. Faults that self-heal (pod-kill: the Deployment
	// recreates the pod) return nil. The harness runs Rollback after the
	// observation window on a fresh context, so it executes even if the run was
	// cancelled.
	Rollback(ctx context.Context) error
}
