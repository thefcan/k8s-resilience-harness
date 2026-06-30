package inject

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// hogPods returns the CPU-hog pods the presser created (those carrying the
// managed-by label), so tests can inspect placement and confirm cleanup.
func hogPods(t *testing.T, cs *fake.Clientset, ns string) []corev1.Pod {
	t.Helper()
	list, err := cs.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{
		LabelSelector: managedByLabel + "=" + managedByHarness,
	})
	if err != nil {
		t.Fatalf("list hog pods: %v", err)
	}
	return list.Items
}

func TestResourcePressureSchedulesHogOnTargetNodeAndCleansUp(t *testing.T) {
	objs := []runtime.Object{
		node("worker1"), node("worker2"),
		podOn("testapp-1", "kresil", "worker1", map[string]string{"app": "testapp"}),
		podOn("testapp-2", "kresil", "worker1", map[string]string{"app": "testapp"}),
		podOn("testapp-3", "kresil", "worker2", map[string]string{"app": "testapp"}),
		podOn("redis-0", "kresil", "worker1", map[string]string{"app": "redis"}), // must not be targeted
	}
	cs := fake.NewSimpleClientset(objs...)
	presser := NewResourcePresser(cs, "kresil", "app=testapp", 2, "500m")

	affected, err := presser.Inject(context.Background())
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}

	hogs := hogPods(t, cs, "kresil")
	if len(hogs) != 1 {
		t.Fatalf("expected exactly one hog pod, got %d", len(hogs))
	}
	hog := hogs[0]

	// The hog must be pinned to a node that actually hosts target pods.
	targetsOn := map[string][]string{
		"worker1": {"testapp-1", "testapp-2"},
		"worker2": {"testapp-3"},
	}
	want, ok := targetsOn[hog.Spec.NodeName]
	if !ok {
		t.Fatalf("hog scheduled on node %q which hosts no target pods", hog.Spec.NodeName)
	}
	if !equalStringSet(affected, want) {
		t.Fatalf("affected = %v, want %v (hog node %s)", affected, want, hog.Spec.NodeName)
	}

	// The hog must be a bounded, pinned, never-restarting CPU burner.
	if hog.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("hog restartPolicy = %q, want Never", hog.Spec.RestartPolicy)
	}
	if len(hog.Spec.Containers) != 1 {
		t.Fatalf("hog has %d containers, want 1", len(hog.Spec.Containers))
	}
	limit := hog.Spec.Containers[0].Resources.Limits.Cpu()
	if limit.IsZero() {
		t.Error("hog has no CPU limit; an unbounded hog could destabilise the host")
	}
	if got := limit.String(); got != "500m" {
		t.Errorf("hog CPU limit = %q, want 500m", got)
	}
	if presser.podName != hog.Name || presser.podName == "" {
		t.Errorf("presser.podName = %q, want the created hog name %q", presser.podName, hog.Name)
	}

	// Rollback deletes the hog and leaves the workload untouched.
	if err := presser.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := hogPods(t, cs, "kresil"); len(got) != 0 {
		t.Fatalf("hog pod still present after rollback: %d", len(got))
	}
	remaining, _ := cs.CoreV1().Pods("kresil").List(context.Background(), metav1.ListOptions{LabelSelector: "app=testapp"})
	if len(remaining.Items) != 3 {
		t.Fatalf("rollback disturbed the workload: %d testapp pods remain, want 3", len(remaining.Items))
	}
}

func TestResourcePressureErrorsWhenNoScheduledTargets(t *testing.T) {
	cs := fake.NewSimpleClientset(
		node("worker1"),
		podOn("redis-0", "kresil", "worker1", map[string]string{"app": "redis"}),
		podOn("testapp-pending", "kresil", "", map[string]string{"app": "testapp"}), // unscheduled
	)
	if _, err := NewResourcePresser(cs, "kresil", "app=testapp", 2, "500m").Inject(context.Background()); err == nil {
		t.Fatal("expected error when no scheduled target pods exist")
	}
	if got := hogPods(t, cs, "kresil"); len(got) != 0 {
		t.Fatalf("no hog should be created when injection fails, got %d", len(got))
	}
}

func TestResourcePressureRollbackWithoutInjectIsNoOp(t *testing.T) {
	cs := fake.NewSimpleClientset(node("worker1"))
	if err := NewResourcePresser(cs, "kresil", "app=testapp", 2, "500m").Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback before Inject should be a no-op, got %v", err)
	}
}
