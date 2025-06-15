package dag

import (
	"clampany/internal"
	"errors"
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v2"
)

type DAG struct {
	Tasks map[string]*internal.Task
	Edges map[string][]string
}

func validateRoleFlows(tasks []internal.Task, roleMap map[string][]string) error {
	taskRole := map[string]string{}
	for _, task := range tasks {
		taskRole[task.Name] = task.Role
	}

	for _, task := range tasks {
		for _, dep := range task.DependsOn {
			fromRole := taskRole[dep]
			toRole := task.Role
			allowed := roleMap[fromRole]
			isAllowed := false
			for _, r := range allowed {
				if r == toRole {
					isAllowed = true
					break
				}
			}
			if !isAllowed {
				return fmt.Errorf("invalid role flow: %s → %s (from task %s to %s)", fromRole, toRole, dep, task.Name)
			}
		}
	}
	return nil
}

func NewDAG(tasks []internal.Task) (*DAG, error) {
	roleData, err := os.ReadFile("roles.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to read roles.yaml: %w", err)
	}
	var roleMap struct {
		AllowedFlows map[string][]string `yaml:"allowed_flows"`
	}
	if err := yaml.Unmarshal(roleData, &roleMap); err != nil {
		return nil, fmt.Errorf("failed to parse roles.yaml: %w", err)
	}

	if err := validateRoleFlows(tasks, roleMap.AllowedFlows); err != nil {
		return nil, err
	}

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
