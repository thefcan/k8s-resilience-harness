package inject

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func pod(name, ns string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels}}
}

func countPods(t *testing.T, cs *fake.Clientset, ns, selector string) int {
	t.Helper()
	list, err := cs.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return len(list.Items)
}

func TestKillDeletesRequestedCount(t *testing.T) {
	objs := []runtime.Object{
		pod("testapp-1", "kresil", map[string]string{"app": "testapp"}),
		pod("testapp-2", "kresil", map[string]string{"app": "testapp"}),
		pod("testapp-3", "kresil", map[string]string{"app": "testapp"}),
		pod("other", "kresil", map[string]string{"app": "redis"}), // must not be touched
	}
	cs := fake.NewSimpleClientset(objs...)
	killer := NewPodKiller(cs, "kresil", "app=testapp", 2)

	killed, err := killer.Inject(context.Background())
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if len(killed) != 2 {
		t.Fatalf("killed %d pods, want 2", len(killed))
	}
	if got := countPods(t, cs, "kresil", "app=testapp"); got != 1 {
		t.Fatalf("remaining testapp pods = %d, want 1", got)
	}
	if got := countPods(t, cs, "kresil", "app=redis"); got != 1 {
		t.Fatalf("redis pod must be untouched, remaining = %d", got)
	}
	for _, name := range killed {
		if name == "other" {
			t.Fatal("killed a pod outside the selector")
		}
	}
}

func TestKillClampsToAvailable(t *testing.T) {
	cs := fake.NewSimpleClientset(
		pod("testapp-1", "kresil", map[string]string{"app": "testapp"}),
		pod("testapp-2", "kresil", map[string]string{"app": "testapp"}),
	)
	killed, err := NewPodKiller(cs, "kresil", "app=testapp", 10).Inject(context.Background())
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if len(killed) != 2 {
		t.Fatalf("killed %d, want 2 (clamped to available)", len(killed))
	}
}

func TestKillErrorsWhenNoMatch(t *testing.T) {
	cs := fake.NewSimpleClientset(pod("testapp-1", "kresil", map[string]string{"app": "testapp"}))
	if _, err := NewPodKiller(cs, "kresil", "app=ghost", 1).Inject(context.Background()); err == nil {
		t.Fatal("expected error when no pods match the selector")
	}
}

func TestPodKillRollbackIsNoOp(t *testing.T) {
	cs := fake.NewSimpleClientset(pod("testapp-1", "kresil", map[string]string{"app": "testapp"}))
	if err := NewPodKiller(cs, "kresil", "app=testapp", 1).Rollback(context.Background()); err != nil {
		t.Fatalf("pod-kill Rollback should be a no-op, got %v", err)
	}
}
