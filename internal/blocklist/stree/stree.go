// Copyright 2012 Thomas Oberndörfer. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package stree implements a segment tree and serial algorithm to query intervals
package stree

import "sort"

// ValueType is the type of a single value in the segment tree.
type ValueType uint32

// Stree represents a Segment Tree.
type Stree struct {
	// Number of intervals
	count ValueType
	root  *node
	// Interval stack
	base []interval
	// Min and max value of all intervals
	min, max ValueType
}

// AddRange pushes new interval to stack
func (t *Stree) AddRange(from, to ValueType) {
	t.base = append(t.base, interval{t.count, segment{from, to}})
	t.count++
}

// Clear the interval stack
func (t *Stree) Clear() {
	t.count = 0
	t.root = nil
	t.base = nil
	t.min = 0
	t.max = 0
}

// Build segment tree out of interval stack
func (t *Stree) Build() {
	if len(t.base) == 0 {
		return
	}
	var es []ValueType
	es, t.min, t.max = endpoints(t.base)
	// Create tree nodes from interval endpoints
	t.root = t.insertNodes(elementaryIntervals(es))
	for i := range t.base {
		t.root.insertInterval(t.base[i])
	}
}

// elementaryIntervals creates a slice of elementary intervals
// from a sorted slice of endpoints
// Input: [p1, p2, ..., pn]
// Output: [{p1 : p1}, {p1 : p2}, {p2 : p2},... , {pn : pn}]
func elementaryIntervals(endpoints []ValueType) []segment {
	intervals := make([]segment, len(endpoints)*2-1)
	for i := 0; i < len(endpoints); i++ {
		intervals[i*2] = segment{endpoints[i], endpoints[i]}
		if i < len(endpoints)-1 { // don't store {pn, pn+1}
			intervals[i*2+1] = segment{endpoints[i], endpoints[i+1]}
		}
	}
	return intervals
}

// endpoints returns a slice with all endpoints (sorted, unique)
func endpoints(base []interval) (result []ValueType, min, max ValueType) {
	baseLen := len(base)
	endpoints := make([]ValueType, baseLen*2)
	for i, interval := range base {
		endpoints[i] = interval.From
		endpoints[i+baseLen] = interval.To
	}
	result = dedup(endpoints)
	min = result[0]
	max = result[len(result)-1]
	return
}

// dedup removes duplicates from a given slice
func dedup(sl []ValueType) []ValueType {
	sort.Slice(sl, func(i, j int) bool { return sl[i] < sl[j] })
	prev := sl[0] + 1
	j := 0
	for i := range sl {
		if sl[i] == prev {
			continue
		}
		sl[j] = sl[i]
		prev = sl[j]
		j++
	}
	return sl[:j]
}

// insertNodes builds the tree structure from the elementary intervals
func (t *Stree) insertNodes(leaves []segment) *node {
	var n *node
	if len(leaves) == 1 {
		n = &node{segment: leaves[0]}
		n.left = nil
		n.right = nil
	} else {
		n = &node{segment: segment{leaves[0].From, leaves[len(leaves)-1].To}}
		center := len(leaves) / 2
		n.left = t.insertNodes(leaves[:center])
		n.right = t.insertNodes(leaves[center:])
	}
	return n
}

// Contains returns truee if value is in segment tree.
func (t Stree) Contains(value ValueType) bool {
	return len(t.query(value, value)) > 0
}

// query interval
func (t Stree) query(from, to ValueType) []interval {
	result := make(map[ValueType]interval)
	if t.root == nil {
		return nil
	}
	t.root.querySingle(from, to, result)
	// transform map to slice
	sl := make([]interval, 0, len(result))
	for _, intrvl := range result {
		sl = append(sl, intrvl)
	}
	return sl
}
