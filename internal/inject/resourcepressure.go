package inject

import (
	"context"
	"fmt"
	"math/rand"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// managedByLabel marks the resources this package creates so they are easy to
// find and clean up, and never collide with the system under test.
const (
	managedByLabel    = "app.kubernetes.io/managed-by"
	managedByHarness  = "k8s-resilience-harness"
	resourcePressJob  = "resource-pressure"
	resourcePressName = "kresil-cpu-hog"
)

// ResourcePresser injects CPU contention onto a node that hosts the target
// workload by scheduling a short-lived CPU-hog pod there, so the workload has to
// compete for CPU. The hog is pinned to the node and capped by a CPU limit, so
// it stresses that node without runaway impact on the host running kind.
//
// Like the other injectors it is selector-scoped: it only contends with the
// experiment's own workload's node, and Rollback deletes the hog pod so the
// cluster is left as it was found.
type ResourcePresser struct {
	client    kubernetes.Interface
	namespace string
	selector  string

	workers  int    // number of busy-loops the hog runs
	cpuLimit string // CPU limit on the hog (e.g. "500m"), bounding host impact

	podName string // hog pod created by Inject, for Rollback to delete
}

// NewResourcePresser constructs a ResourcePresser. workers is how many CPU
// busy-loops the hog spawns; cpuLimit (a Kubernetes quantity such as "500m" or
// "1") caps how much CPU the hog may actually use, keeping the host safe.
func NewResourcePresser(client kubernetes.Interface, namespace, selector string, workers int, cpuLimit string) *ResourcePresser {
	return &ResourcePresser{
		client:    client,
		namespace: namespace,
		selector:  selector,
		workers:   workers,
		cpuLimit:  cpuLimit,
	}
}

// Inject picks a node hosting the target workload and schedules a CPU-hog pod
// pinned to it. It returns the target pods sharing that node — the ones that now
// have to compete for CPU.
func (r *ResourcePresser) Inject(ctx context.Context) ([]string, error) {
	pods, err := r.client.CoreV1().Pods(r.namespace).List(ctx, metav1.ListOptions{LabelSelector: r.selector})
	if err != nil {
		return nil, fmt.Errorf("list pods (ns=%s selector=%q): %w", r.namespace, r.selector, err)
	}

	// Group running, non-terminating target pods by the node they run on, so the
	// hog lands where the workload actually is.
	byNode := map[string][]string{}
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.DeletionTimestamp != nil || p.Spec.NodeName == "" {
			continue // skip terminating or not-yet-scheduled pods
		}
		byNode[p.Spec.NodeName] = append(byNode[p.Spec.NodeName], p.Name)
	}
	if len(byNode) == 0 {
		return nil, fmt.Errorf("no scheduled target pods (selector %q in namespace %q) found on any node", r.selector, r.namespace)
	}

	nodes := make([]string, 0, len(byNode))
	for n := range byNode {
		nodes = append(nodes, n)
	}
	rand.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })
	node := nodes[0]

	created, err := r.client.CoreV1().Pods(r.namespace).Create(ctx, r.hogPod(node), metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create CPU-hog pod on node %s: %w", node, err)
	}
	r.podName = created.Name
	return byNode[node], nil
}

// Rollback deletes the CPU-hog pod, tolerating one that is already gone.
func (r *ResourcePresser) Rollback(ctx context.Context) error {
	if r.podName == "" {
		return nil
	}
	err := r.client.CoreV1().Pods(r.namespace).Delete(ctx, r.podName, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete CPU-hog pod %s: %w", r.podName, err)
	}
	return nil
}

// hogPod builds the CPU-hog pod, pinned to the given node and capped by cpuLimit.
// The container spawns `workers` shell busy-loops and waits on them; the cgroup
// CPU limit is what actually bounds the pressure regardless of the loop count.
func (r *ResourcePresser) hogPod(node string) *corev1.Pod {
	script := fmt.Sprintf(
		`n=%d; i=0; while [ "$i" -lt "$n" ]; do (while true; do :; done) & i=$((i+1)); done; wait`,
		r.workers,
	)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourcePressName + "-" + randSuffix(),
			Namespace: r.namespace,
			Labels: map[string]string{
				managedByLabel:                 managedByHarness,
				"k8s-resilience-harness/fault": resourcePressJob,
			},
		},
		Spec: corev1.PodSpec{
			NodeName:      node,
			RestartPolicy: corev1.RestartPolicyNever,
			// Tolerate a brief cordon/taint race; the hog is short-lived anyway.
			TerminationGracePeriodSeconds: ptr(int64(0)),
			Containers: []corev1.Container{{
				Name:    "cpu-hog",
				Image:   "busybox:1.36",
				Command: []string{"sh", "-c", script},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m")},
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(r.cpuLimit)},
				},
			}},
		},
	}
}

// randSuffix returns a short random alphanumeric string for unique resource names.
func randSuffix() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 5)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func ptr[T any](v T) *T { return &v }
