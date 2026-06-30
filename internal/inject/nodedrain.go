package inject

import (
	"context"
	"fmt"
	"math/rand"

	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NodeDrainer simulates a node going away: it cordons a node that hosts the
// target workload (so nothing reschedules back onto it) and evicts that node's
// target pods via the Eviction API, forcing them to reschedule elsewhere.
//
// It is selector-scoped on purpose — it disrupts the experiment's own workload,
// not arbitrary tenants (system or storage pods) sharing the node. Rollback
// uncordons the node so the experiment is repeatable.
type NodeDrainer struct {
	client    kubernetes.Interface
	namespace string
	selector  string

	cordoned string // node cordoned by Inject, for Rollback to undo
}

// NewNodeDrainer constructs a NodeDrainer for the given namespace/selector.
func NewNodeDrainer(client kubernetes.Interface, namespace, selector string) *NodeDrainer {
	return &NodeDrainer{client: client, namespace: namespace, selector: selector}
}

// Inject picks a node hosting the target workload, cordons it, and evicts every
// target pod scheduled there. It returns the names of the evicted pods.
func (d *NodeDrainer) Inject(ctx context.Context) ([]string, error) {
	pods, err := d.client.CoreV1().Pods(d.namespace).List(ctx, metav1.ListOptions{LabelSelector: d.selector})
	if err != nil {
		return nil, fmt.Errorf("list pods (ns=%s selector=%q): %w", d.namespace, d.selector, err)
	}

	// Group running, non-terminating target pods by the node they run on.
	byNode := map[string][]string{}
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.DeletionTimestamp != nil || p.Spec.NodeName == "" {
			continue // skip terminating or not-yet-scheduled pods
		}
		byNode[p.Spec.NodeName] = append(byNode[p.Spec.NodeName], p.Name)
	}
	if len(byNode) == 0 {
		return nil, fmt.Errorf("no scheduled target pods (selector %q in namespace %q) found on any node", d.selector, d.namespace)
	}

	// Pick a node at random among those hosting target pods.
	nodes := make([]string, 0, len(byNode))
	for n := range byNode {
		nodes = append(nodes, n)
	}
	rand.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })
	node := nodes[0]

	if err := d.cordon(ctx, node); err != nil {
		return nil, err
	}

	evicted := make([]string, 0, len(byNode[node]))
	for _, name := range byNode[node] {
		if err := d.evict(ctx, name); err != nil {
			return evicted, fmt.Errorf("evict pod %s from node %s: %w", name, node, err)
		}
		evicted = append(evicted, name)
	}
	return evicted, nil
}

// Rollback uncordons the node Inject cordoned, so the cluster is left schedulable
// and the experiment can run again.
func (d *NodeDrainer) Rollback(ctx context.Context) error {
	if d.cordoned == "" {
		return nil
	}
	node, err := d.client.CoreV1().Nodes().Get(ctx, d.cordoned, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get node %s: %w", d.cordoned, err)
	}
	if !node.Spec.Unschedulable {
		return nil
	}
	node.Spec.Unschedulable = false
	if _, err := d.client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("uncordon node %s: %w", d.cordoned, err)
	}
	return nil
}

// cordon marks the node unschedulable and records it for Rollback.
func (d *NodeDrainer) cordon(ctx context.Context, name string) error {
	node, err := d.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get node %s: %w", name, err)
	}
	if !node.Spec.Unschedulable {
		node.Spec.Unschedulable = true
		if _, err := d.client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("cordon node %s: %w", name, err)
		}
	}
	d.cordoned = name
	return nil
}

// evict requests eviction of a single pod, tolerating a pod that is already gone.
func (d *NodeDrainer) evict(ctx context.Context, podName string) error {
	eviction := &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: d.namespace},
	}
	err := d.client.PolicyV1().Evictions(d.namespace).Evict(ctx, eviction)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
