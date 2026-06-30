package inject

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func statefulSet(name, ns string, replicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
	}
}

func stsReplicas(t *testing.T, cs *fake.Clientset, name, ns string) int32 {
	t.Helper()
	sts, err := cs.AppsV1().StatefulSets(ns).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get statefulset %s/%s: %v", ns, name, err)
	}
	if sts.Spec.Replicas == nil {
		return 0
	}
	return *sts.Spec.Replicas
}

func TestPartitionScalesDependencyToZeroAndRestores(t *testing.T) {
	cs := fake.NewSimpleClientset(statefulSet("redis", "kresil", 1))
	p := NewDependencyPartitioner(cs, "kresil", "redis")

	affected, err := p.Inject(context.Background())
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if len(affected) != 1 || affected[0] != "redis" {
		t.Fatalf("affected = %v, want [redis]", affected)
	}
	if got := stsReplicas(t, cs, "redis", "kresil"); got != 0 {
		t.Fatalf("redis replicas after Inject = %d, want 0 (partitioned)", got)
	}

	if err := p.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := stsReplicas(t, cs, "redis", "kresil"); got != 1 {
		t.Fatalf("redis replicas after Rollback = %d, want 1 (restored)", got)
	}
}

func TestPartitionRestoresOriginalReplicaCount(t *testing.T) {
	// A dependency that ran with 3 replicas must come back at 3, not a hard-coded 1.
	cs := fake.NewSimpleClientset(statefulSet("redis", "kresil", 3))
	p := NewDependencyPartitioner(cs, "kresil", "redis")

	if _, err := p.Inject(context.Background()); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if got := stsReplicas(t, cs, "redis", "kresil"); got != 0 {
		t.Fatalf("redis replicas after Inject = %d, want 0", got)
	}
	if err := p.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := stsReplicas(t, cs, "redis", "kresil"); got != 3 {
		t.Fatalf("redis replicas after Rollback = %d, want 3", got)
	}
}

func TestPartitionErrorsWhenDependencyMissing(t *testing.T) {
	cs := fake.NewSimpleClientset() // no statefulset
	if _, err := NewDependencyPartitioner(cs, "kresil", "redis").Inject(context.Background()); err == nil {
		t.Fatal("expected error when the dependency statefulset does not exist")
	}
}

func TestPartitionRollbackWithoutInjectIsNoOp(t *testing.T) {
	cs := fake.NewSimpleClientset(statefulSet("redis", "kresil", 1))
	if err := NewDependencyPartitioner(cs, "kresil", "redis").Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback before Inject should be a no-op, got %v", err)
	}
	if got := stsReplicas(t, cs, "redis", "kresil"); got != 1 {
		t.Fatalf("redis replicas disturbed by no-op rollback = %d, want 1", got)
	}
}
