package inject

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func node(name string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func podOn(name, ns, nodeName string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec:       corev1.PodSpec{NodeName: nodeName},
	}
}

// cordonedNodes returns the names of all nodes currently marked unschedulable.
func cordonedNodes(t *testing.T, cs *fake.Clientset) []string {
	t.Helper()
	list, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	var out []string
	for i := range list.Items {
		if list.Items[i].Spec.Unschedulable {
			out = append(out, list.Items[i].Name)
		}
	}
	return out
}

// evictionNames returns the pod names the harness asked the Eviction API to
// evict. The fake clientset records the eviction as an action rather than
// deleting the pod, so we read it from the action log.
func evictionNames(t *testing.T, cs *fake.Clientset) []string {
	t.Helper()
	var names []string
	for _, a := range cs.Actions() {
		if a.GetVerb() != "create" || a.GetSubresource() != "eviction" {
			continue
		}
		ca, ok := a.(clienttesting.CreateAction)
		if !ok {
			t.Fatalf("eviction action is not a CreateAction: %T", a)
		}
		ev, ok := ca.GetObject().(*policyv1.Eviction)
		if !ok {
			t.Fatalf("eviction object is not *policyv1.Eviction: %T", ca.GetObject())
		}
		names = append(names, ev.Name)
	}
	return names
}

func equalStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestDrainCordonsNodeAndEvictsTargetPods(t *testing.T) {
	objs := []runtime.Object{
		node("worker1"), node("worker2"),
		podOn("testapp-1", "kresil", "worker1", map[string]string{"app": "testapp"}),
		podOn("testapp-2", "kresil", "worker1", map[string]string{"app": "testapp"}),
		podOn("testapp-3", "kresil", "worker2", map[string]string{"app": "testapp"}),
		podOn("redis-0", "kresil", "worker1", map[string]string{"app": "redis"}), // must not be touched
	}
	cs := fake.NewSimpleClientset(objs...)
	drainer := NewNodeDrainer(cs, "kresil", "app=testapp")

	evicted, err := drainer.Inject(context.Background())
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}

	cordoned := cordonedNodes(t, cs)
	if len(cordoned) != 1 {
		t.Fatalf("expected exactly one cordoned node, got %v", cordoned)
	}
	targetsOn := map[string][]string{
		"worker1": {"testapp-1", "testapp-2"},
		"worker2": {"testapp-3"},
	}
	want := targetsOn[cordoned[0]]

	if !equalStringSet(evicted, want) {
		t.Fatalf("evicted = %v, want %v (cordoned node %s)", evicted, want, cordoned[0])
	}
	if got := evictionNames(t, cs); !equalStringSet(got, want) {
		t.Fatalf("eviction API calls = %v, want %v", got, want)
	}
	for _, n := range evicted {
		if n == "redis-0" {
			t.Fatal("drain evicted a pod outside the selector (redis)")
		}
	}

	// Rollback must uncordon the node, leaving the cluster schedulable again.
	if err := drainer.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := cordonedNodes(t, cs); len(got) != 0 {
		t.Fatalf("nodes still cordoned after rollback: %v", got)
	}
}

func TestDrainErrorsWhenNoScheduledTargets(t *testing.T) {
	cs := fake.NewSimpleClientset(
		node("worker1"),
		podOn("redis-0", "kresil", "worker1", map[string]string{"app": "redis"}),
		podOn("testapp-pending", "kresil", "", map[string]string{"app": "testapp"}), // unscheduled
	)
	if _, err := NewNodeDrainer(cs, "kresil", "app=testapp").Inject(context.Background()); err == nil {
		t.Fatal("expected error when no scheduled target pods exist")
	}
	if got := cordonedNodes(t, cs); len(got) != 0 {
		t.Fatalf("no node should be cordoned when injection fails, got %v", got)
	}
}

func TestDrainRollbackWithoutInjectIsNoOp(t *testing.T) {
	cs := fake.NewSimpleClientset(node("worker1"))
	if err := NewNodeDrainer(cs, "kresil", "app=testapp").Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback before Inject should be a no-op, got %v", err)
	}
}
