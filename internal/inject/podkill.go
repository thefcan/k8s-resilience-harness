package inject

import (
	"context"
	"fmt"
	"math/rand"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PodKiller deletes up to count pods matching a label selector in a namespace.
// It depends only on kubernetes.Interface, so tests drive it with a fake
// clientset.
type PodKiller struct {
	client    kubernetes.Interface
	namespace string
	selector  string
	count     int
}

// NewPodKiller constructs a PodKiller that deletes count pods per injection.
func NewPodKiller(client kubernetes.Interface, namespace, selector string, count int) *PodKiller {
	return &PodKiller{client: client, namespace: namespace, selector: selector, count: count}
}

// Inject deletes up to count pods matching the selector (chosen at random among
// pods that are not already terminating) and returns the names it deleted.
func (p *PodKiller) Inject(ctx context.Context) ([]string, error) {
	pods, err := p.client.CoreV1().Pods(p.namespace).List(ctx, metav1.ListOptions{LabelSelector: p.selector})
	if err != nil {
		return nil, fmt.Errorf("list pods (ns=%s selector=%q): %w", p.namespace, p.selector, err)
	}

	candidates := make([]string, 0, len(pods.Items))
	for i := range pods.Items {
		if pods.Items[i].DeletionTimestamp == nil { // skip pods already terminating
			candidates = append(candidates, pods.Items[i].Name)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no killable pods match selector %q in namespace %q", p.selector, p.namespace)
	}

	rand.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	count := p.count
	if count > len(candidates) {
		count = len(candidates)
	}

	killed := make([]string, 0, count)
	for _, name := range candidates[:count] {
		if err := p.client.CoreV1().Pods(p.namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			return killed, fmt.Errorf("delete pod %s: %w", name, err)
		}
		killed = append(killed, name)
	}
	return killed, nil
}

// Rollback is a no-op: the Deployment recreates the killed pods on its own.
func (*PodKiller) Rollback(context.Context) error { return nil }
