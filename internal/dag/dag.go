package dag

import (
	"clampany/internal"
	"errors"
	"sort"
)

type DAG struct {
	Tasks map[string]*internal.Task
	Edges map[string][]string
}

func NewDAG(tasks []internal.Task) (*DAG, error) {
	d := &DAG{
		Tasks: map[string]*internal.Task{},
		Edges: map[string][]string{},
	}
	for i, t := range tasks {
		t := t // コピー
		d.Tasks[t.Name] = &tasks[i]
		d.Edges[t.Name] = t.DependsOn
	}
	if hasCycle(d) {
		return nil, errors.New("cycle detected in task dependencies")
	}
	return d, nil
}

func hasCycle(d *DAG) bool {
	visited := make(map[string]bool)
	stack := make(map[string]bool)
	var visit func(string) bool
	visit = func(n string) bool {
		if stack[n] {
			return true // サイクル
		}
		if visited[n] {
			return false
		}
		visited[n] = true
		stack[n] = true
		for _, dep := range d.Edges[n] {
			if visit(dep) {
				return true
			}
		}
		stack[n] = false
		return false
	}
	for n := range d.Tasks {
		if visit(n) {
			return true
		}
	}
	return false
}

func TopoSort(d *DAG) ([]string, error) {
	inDegree := make(map[string]int)
	children := make(map[string][]string)
	for n := range d.Tasks {
		inDegree[n] = 0
	}
	for n, deps := range d.Edges {
		for _, dep := range deps {
			inDegree[n]++
			children[dep] = append(children[dep], n)
		}
	}
	var queue []string
	var zeroNodes []string
	for n, deg := range inDegree {
		if deg == 0 {
			zeroNodes = append(zeroNodes, n)
		}
	}
	sort.Strings(zeroNodes)
	queue = append(queue, zeroNodes...)
	var order []string
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		order = append(order, n)
		childs := append([]string{}, children[n]...)
		sort.Strings(childs)
		for _, child := range childs {
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}
	if len(order) != len(d.Tasks) {
		return nil, errors.New("cycle detected in tasks (toposort)")
	}
	return order, nil
}
