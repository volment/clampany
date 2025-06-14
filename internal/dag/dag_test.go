package dag

import (
	"clampany/internal"
	"testing"
)

func TestDAG_TopoSort(t *testing.T) {
	tasks := []internal.Task{
		{Name: "a", DependsOn: []string{}},
		{Name: "b", DependsOn: []string{"a"}},
		{Name: "c", DependsOn: []string{"b"}},
	}
	d, err := NewDAG(tasks)
	if err != nil {
		t.Fatalf("DAG構築失敗: %v", err)
	}
	order, err := TopoSort(d)
	if err != nil {
		t.Fatalf("トポロジカルソート失敗: %v", err)
	}
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Errorf("順序不正: %v", order)
	}
}

func TestDAG_Cycle(t *testing.T) {
	tasks := []internal.Task{
		{Name: "a", DependsOn: []string{"c"}},
		{Name: "b", DependsOn: []string{"a"}},
		{Name: "c", DependsOn: []string{"b"}},
	}
	_, err := NewDAG(tasks)
	if err == nil {
		t.Error("サイクル検出できていない")
	}
}
