// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
// Package transform - DAG executor for transform pipelines.
//
// When transform steps declare ID, Input, or DependsOn fields, they form a
// directed acyclic graph rather than a linear pipeline. The DAG executor
// resolves dependencies, detects cycles, and runs independent branches in
// parallel using goroutines.
package transform

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// DAGExecutor runs transform steps as a directed acyclic graph.
type DAGExecutor struct {
	steps   []Step
	byID    map[string]int // step ID -> index in steps
	adjList map[string][]string // step ID -> IDs it depends on
}

// NewDAGExecutor creates a DAG executor if any steps have DAG fields (ID, Input, DependsOn).
// Returns nil if all steps are linear (no DAG fields), meaning the caller should use
// the sequential Pipeline instead.
func NewDAGExecutor(steps []Step) (*DAGExecutor, error) {
	hasDAG := false
	for _, s := range steps {
		if s.ID != "" || s.Input != "" || len(s.DependsOn) > 0 {
			hasDAG = true
			break
		}
	}
	if !hasDAG {
		return nil, nil
	}

	// Assign IDs to steps that lack one (step_0, step_1, ...)
	normalized := make([]Step, len(steps))
	copy(normalized, steps)
	for i := range normalized {
		if normalized[i].ID == "" {
			normalized[i].ID = fmt.Sprintf("step_%d", i)
		}
	}

	// Build index
	byID := make(map[string]int, len(normalized))
	for i, s := range normalized {
		if _, dup := byID[s.ID]; dup {
			return nil, fmt.Errorf("dag: duplicate step ID %q", s.ID)
		}
		byID[s.ID] = i
	}

	// Build adjacency list (dependencies)
	adjList := make(map[string][]string, len(normalized))
	for _, s := range normalized {
		var deps []string
		if s.Input != "" {
			if _, ok := byID[s.Input]; !ok {
				return nil, fmt.Errorf("dag: step %q references unknown input %q", s.ID, s.Input)
			}
			deps = append(deps, s.Input)
		}
		for _, dep := range s.DependsOn {
			if _, ok := byID[dep]; !ok {
				return nil, fmt.Errorf("dag: step %q references unknown dependency %q", s.ID, dep)
			}
			deps = append(deps, dep)
		}
		// Merge steps reference their sources as dependencies
		if s.Type == "merge" {
			for _, src := range s.Sources {
				if _, ok := byID[src]; !ok {
					return nil, fmt.Errorf("dag: merge step %q references unknown source %q", s.ID, src)
				}
				deps = append(deps, src)
			}
		}
		adjList[s.ID] = deps
	}

	d := &DAGExecutor{
		steps:   normalized,
		byID:    byID,
		adjList: adjList,
	}

	if err := d.detectCycles(); err != nil {
		return nil, err
	}

	return d, nil
}

// Execute runs the DAG, starting with initialData as the virtual root input.
// Steps run in topological order. Independent branches execute concurrently.
func (d *DAGExecutor) Execute(ctx context.Context, initialData any) (any, error) {
	order, err := d.topologicalSort()
	if err != nil {
		return nil, err
	}

	outputs := make(map[string]any, len(d.steps)+1)
	outputs["_root"] = initialData

	// Group steps into layers: each layer contains steps whose dependencies
	// are all satisfied by previous layers.
	completed := map[string]bool{"_root": true}
	remaining := make([]string, len(order))
	copy(remaining, order)

	for len(remaining) > 0 {
		var ready []Step
		var readyIDs []string
		var notReady []string

		for _, id := range remaining {
			deps := d.adjList[id]
			allDone := true
			for _, dep := range deps {
				if !completed[dep] {
					allDone = false
					break
				}
			}
			if allDone {
				ready = append(ready, d.steps[d.byID[id]])
				readyIDs = append(readyIDs, id)
			} else {
				notReady = append(notReady, id)
			}
		}

		if len(ready) == 0 {
			return nil, fmt.Errorf("dag: deadlock, no steps ready to execute from %v", remaining)
		}

		if err := d.executeLayer(ctx, ready, outputs); err != nil {
			return nil, err
		}

		for _, id := range readyIDs {
			completed[id] = true
		}
		remaining = notReady
	}

	// Return the last step's output (based on topological order)
	lastID := order[len(order)-1]
	return outputs[lastID], nil
}

// executeLayer runs a set of independent steps in parallel.
func (d *DAGExecutor) executeLayer(ctx context.Context, ready []Step, outputs map[string]any) error {
	if len(ready) == 1 {
		return d.executeSingle(ctx, ready[0], outputs)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, step := range ready {
		wg.Add(1)
		go func(s Step) {
			defer wg.Done()

			result, err := d.executeOneStep(ctx, s, outputs)

			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			outputs[s.ID] = result
		}(step)
	}
	wg.Wait()
	return firstErr
}

// executeSingle runs a single step (no goroutine overhead).
func (d *DAGExecutor) executeSingle(ctx context.Context, step Step, outputs map[string]any) error {
	result, err := d.executeOneStep(ctx, step, outputs)
	if err != nil {
		return err
	}
	outputs[step.ID] = result
	return nil
}

// executeOneStep runs a single step, handling When, Each, and Merge.
func (d *DAGExecutor) executeOneStep(ctx context.Context, step Step, outputs map[string]any) (any, error) {
	// Handle merge type specially: it reads from multiple sources
	if step.Type == "merge" {
		return d.applyMerge(step, outputs)
	}

	// Resolve input data
	input := d.resolveInput(step, outputs)

	// Evaluate When condition
	if step.When != "" {
		if !evaluateWhen(step.When, input) {
			return nil, nil
		}
	}

	// Handle Each (fan-out)
	if step.Each {
		return d.executeFanOut(ctx, step, input)
	}

	// Normal step
	return applyStep(step, input)
}

// resolveInput determines the input data for a step.
// Priority: explicit Input field > _root (if no dependencies).
func (d *DAGExecutor) resolveInput(step Step, outputs map[string]any) any {
	if step.Input != "" {
		return outputs[step.Input]
	}
	// If the step has explicit DependsOn but no Input, use _root
	// (the step only needs ordering, not data flow from deps)
	if len(d.adjList[step.ID]) == 0 {
		return outputs["_root"]
	}
	// Default: use the first dependency's output as input
	deps := d.adjList[step.ID]
	if len(deps) > 0 {
		return outputs[deps[0]]
	}
	return outputs["_root"]
}

// executeFanOut iterates over an input array, running the step on each element
// with bounded concurrency.
func (d *DAGExecutor) executeFanOut(ctx context.Context, step Step, input any) (any, error) {
	arr, ok := input.([]any)
	if !ok {
		return nil, fmt.Errorf("each: step %q input must be an array, got %T", step.ID, input)
	}

	concurrency := step.Concurrency
	if concurrency <= 0 {
		concurrency = 10
	}

	results := make([]any, len(arr))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i, item := range arr {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, data any) {
			defer wg.Done()
			defer func() { <-sem }()

			result, err := applyStep(step, data)

			mu.Lock()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			results[idx] = result
			mu.Unlock()
		}(i, item)
	}
	wg.Wait()
	return results, firstErr
}

// ---------------------------------------------------------------------------
// Merge strategies
// ---------------------------------------------------------------------------

func (d *DAGExecutor) applyMerge(step Step, outputs map[string]any) (any, error) {
	sources := make([]any, len(step.Sources))
	for i, src := range step.Sources {
		sources[i] = outputs[src]
	}

	switch step.Strategy {
	case "zip":
		return mergeZip(sources)
	case "concat":
		return mergeConcat(sources)
	case "first":
		return mergeFirst(sources)
	case "join":
		return mergeJoin(sources, step.JoinOn)
	case "object":
		return mergeObject(step.Sources, sources)
	default:
		return mergeConcat(sources)
	}
}

// mergeConcat concatenates arrays from all sources into one array.
func mergeConcat(sources []any) (any, error) {
	var result []any
	for _, src := range sources {
		if src == nil {
			continue
		}
		if arr, ok := src.([]any); ok {
			result = append(result, arr...)
		} else {
			result = append(result, src)
		}
	}
	if result == nil {
		result = []any{}
	}
	return result, nil
}

// mergeZip interleaves arrays element-by-element, producing an array of pairs.
func mergeZip(sources []any) (any, error) {
	if len(sources) == 0 {
		return []any{}, nil
	}

	// Find the shortest array length
	arrays := make([][]any, 0, len(sources))
	minLen := -1
	for _, src := range sources {
		if src == nil {
			arrays = append(arrays, nil)
			continue
		}
		arr, ok := src.([]any)
		if !ok {
			arr = []any{src}
		}
		arrays = append(arrays, arr)
		if minLen < 0 || len(arr) < minLen {
			minLen = len(arr)
		}
	}
	if minLen <= 0 {
		return []any{}, nil
	}

	result := make([]any, minLen)
	for i := 0; i < minLen; i++ {
		tuple := make([]any, len(arrays))
		for j, arr := range arrays {
			if arr != nil && i < len(arr) {
				tuple[j] = arr[i]
			}
		}
		result[i] = tuple
	}
	return result, nil
}

// mergeFirst returns the first non-nil source.
func mergeFirst(sources []any) (any, error) {
	for _, src := range sources {
		if src != nil {
			return src, nil
		}
	}
	return nil, nil
}

// mergeJoin performs a key-based join across source arrays.
func mergeJoin(sources []any, joinOn string) (any, error) {
	if joinOn == "" {
		return nil, fmt.Errorf("merge join requires join_on field")
	}
	if len(sources) == 0 {
		return []any{}, nil
	}

	// Index the first source by the join key
	index := make(map[string]map[string]any)
	if first, ok := sources[0].([]any); ok {
		for _, item := range first {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			key := fmt.Sprintf("%v", obj[joinOn])
			index[key] = obj
		}
	}

	// Merge subsequent sources into matching records
	for _, src := range sources[1:] {
		arr, ok := src.([]any)
		if !ok {
			continue
		}
		for _, item := range arr {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			key := fmt.Sprintf("%v", obj[joinOn])
			if base, exists := index[key]; exists {
				for k, v := range obj {
					if _, already := base[k]; !already {
						base[k] = v
					}
				}
			}
		}
	}

	result := make([]any, 0, len(index))
	for _, obj := range index {
		result = append(result, obj)
	}
	return result, nil
}

// mergeObject combines sources into a single object keyed by source ID.
func mergeObject(names []string, sources []any) (any, error) {
	result := make(map[string]any, len(names))
	for i, name := range names {
		if i < len(sources) {
			result[name] = sources[i]
		}
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// When condition evaluator
// ---------------------------------------------------------------------------

// evaluateWhen evaluates a simple condition expression against input data.
// Supported forms:
//   - size(data) == N, size(data) > N, size(data) < N, etc.
//   - data.field == 'value', data.field != 'value'
func evaluateWhen(when string, data any) bool {
	when = strings.TrimSpace(when)

	// size(data) op N
	if strings.HasPrefix(when, "size(data)") {
		return evaluateSizeCondition(when, data)
	}

	// data.field op value
	if strings.HasPrefix(when, "data.") {
		return evaluateFieldCondition(when, data)
	}

	return true // unknown expression, default to running the step
}

var sizeRe = regexp.MustCompile(`^size\(data\)\s*(==|!=|>=|<=|>|<)\s*(\d+)$`)

func evaluateSizeCondition(when string, data any) bool {
	matches := sizeRe.FindStringSubmatch(when)
	if matches == nil {
		return true
	}

	op := matches[1]
	target, _ := strconv.Atoi(matches[2])

	var size int
	switch v := data.(type) {
	case []any:
		size = len(v)
	case map[string]any:
		size = len(v)
	case string:
		size = len(v)
	case nil:
		size = 0
	default:
		return true
	}

	switch op {
	case "==":
		return size == target
	case "!=":
		return size != target
	case ">":
		return size > target
	case "<":
		return size < target
	case ">=":
		return size >= target
	case "<=":
		return size <= target
	}
	return true
}

var fieldCondRe = regexp.MustCompile(`^data\.(\S+)\s*(==|!=)\s*'([^']*)'$`)

func evaluateFieldCondition(when string, data any) bool {
	matches := fieldCondRe.FindStringSubmatch(when)
	if matches == nil {
		return true
	}

	field := matches[1]
	op := matches[2]
	target := matches[3]

	obj, ok := data.(map[string]any)
	if !ok {
		return false
	}

	// Navigate dotted field path
	parts := strings.Split(field, ".")
	var current any = obj
	for _, p := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current = m[p]
	}

	val := fmt.Sprintf("%v", current)
	switch op {
	case "==":
		return val == target
	case "!=":
		return val != target
	}
	return true
}

// ---------------------------------------------------------------------------
// Cycle detection
// ---------------------------------------------------------------------------

func (d *DAGExecutor) detectCycles() error {
	const (
		white = 0 // unvisited
		gray  = 1 // in progress
		black = 2 // done
	)

	color := make(map[string]int, len(d.steps))
	parent := make(map[string]string, len(d.steps))
	for _, s := range d.steps {
		color[s.ID] = white
	}

	var dfs func(id string) error
	dfs = func(id string) error {
		color[id] = gray
		for _, dep := range d.adjList[id] {
			if color[dep] == gray {
				// Build cycle path
				cycle := []string{dep, id}
				cur := id
				for cur != dep {
					p, ok := parent[cur]
					if !ok {
						break
					}
					cycle = append(cycle, p)
					cur = p
				}
				// Reverse for readability
				for i, j := 0, len(cycle)-1; i < j; i, j = i+1, j-1 {
					cycle[i], cycle[j] = cycle[j], cycle[i]
				}
				return fmt.Errorf("dag: cycle detected: %s", strings.Join(cycle, " -> "))
			}
			if color[dep] == white {
				parent[dep] = id
				if err := dfs(dep); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}

	for _, s := range d.steps {
		if color[s.ID] == white {
			if err := dfs(s.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Topological sort
// ---------------------------------------------------------------------------

func (d *DAGExecutor) topologicalSort() ([]string, error) {
	inDegree := make(map[string]int, len(d.steps))
	for _, s := range d.steps {
		inDegree[s.ID] = 0
	}

	// Reverse adjacency: for each dependency, increment the dependent's in-degree.
	// adjList maps step -> its dependencies, so we need the reverse.
	reverse := make(map[string][]string, len(d.steps))
	for id, deps := range d.adjList {
		for _, dep := range deps {
			reverse[dep] = append(reverse[dep], id)
		}
		// Ensure the entry exists
		if _, ok := reverse[id]; !ok {
			reverse[id] = nil
		}
	}

	// Kahn's algorithm
	var queue []string
	for _, s := range d.steps {
		if len(d.adjList[s.ID]) == 0 {
			queue = append(queue, s.ID)
		} else {
			inDegree[s.ID] = len(d.adjList[s.ID])
		}
	}

	var order []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)

		for _, dependent := range reverse[id] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(order) != len(d.steps) {
		return nil, fmt.Errorf("dag: topological sort incomplete, possible cycle")
	}
	return order, nil
}

// ---------------------------------------------------------------------------
// Mermaid diagram output
// ---------------------------------------------------------------------------

// Mermaid returns a Mermaid diagram string representing the DAG structure.
func (d *DAGExecutor) Mermaid() string {
	var sb strings.Builder
	sb.WriteString("graph TD\n")
	for _, step := range d.steps {
		typeName := step.Type
		if typeName == "" {
			typeName = "step"
		}
		label := fmt.Sprintf("  %s[%s: %s]\n", step.ID, typeName, step.ID)
		sb.WriteString(label)

		if step.Input != "" {
			sb.WriteString(fmt.Sprintf("  %s --> %s\n", step.Input, step.ID))
		}
		for _, dep := range step.DependsOn {
			sb.WriteString(fmt.Sprintf("  %s --> %s\n", dep, step.ID))
		}
		if step.Type == "merge" {
			for _, src := range step.Sources {
				sb.WriteString(fmt.Sprintf("  %s --> %s\n", src, step.ID))
			}
		}
	}
	return sb.String()
}
