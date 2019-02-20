// Copyright 2012 Thomas Oberndörfer. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stree

import "testing"

func TestEmptyTree(t *testing.T) {
	var tree Stree
	tree.Build()
	result := tree.query(0, 0)
	if len(result) != 0 {
		t.Errorf("fail query empty tree")
	}
	result = tree.query(2, 3)
	if len(result) != 0 {
		t.Errorf("fail query empty tree")
	}
}

func TestMinimalTree(t *testing.T) {
	var tree Stree
	tree.AddRange(3, 7)
	tree.Build()
	result := tree.query(1, 2)
	if len(result) != 0 {
		t.Errorf("fail query minimal tree")
	}
	result = tree.query(2, 3)
	if len(result) != 1 {
		t.Errorf("fail query minimal tree")
	}
}

func TestMinimalTree2(t *testing.T) {
	var tree Stree
	tree.AddRange(1, 1)
	tree.Build()
	if result := tree.query(1, 1); len(result) != 1 {
		t.Errorf("fail query minimal tree for (1, 1)")
	}
	if result := tree.query(1, 2); len(result) != 1 {
		t.Errorf("fail query minimal tree for (1, 2)")
	}
	if result := tree.query(2, 3); len(result) != 0 {
		t.Errorf("fail query minimal tree for (2, 3)")
	}
}

func TestNormalTree(t *testing.T) {
	var tree Stree
	tree.AddRange(1, 1)
	tree.AddRange(2, 3)
	tree.AddRange(5, 7)
	tree.AddRange(4, 6)
	tree.AddRange(6, 9)
	tree.Build()
	if result := tree.query(3, 5); len(result) != 3 {
		t.Errorf("fail query multiple tree for (3, 5)")
	}
	qvalid := map[ValueType]int{
		0: 0,
		1: 1,
		2: 1,
		3: 1,
		4: 1,
		5: 2,
		6: 3,
		7: 2,
		8: 1,
		9: 1,
	}
	for i := ValueType(0); i <= 9; i++ {
		if result := tree.query(i, i); len(result) != qvalid[i] {
			t.Errorf("fail query multiple tree for (%d, %d)", i, i)
		}
	}
}

func TestContains(t *testing.T) {
	var tree Stree
	tree.AddRange(2, 4)
	tree.Build()
	if tree.Contains(1) {
		t.Errorf("fail")
	}
	if !tree.Contains(2) {
		t.Errorf("fail")
	}
	if !tree.Contains(3) {
		t.Errorf("fail")
	}
	if !tree.Contains(4) {
		t.Errorf("fail")
	}
	if tree.Contains(5) {
		t.Errorf("fail")
	}
}

func TestDedup(t *testing.T) {
	l := []ValueType{1, 2, 3, 3, 4, 4, 4}
	l2 := dedup(l)
	if len(l2) != 4 {
		t.Errorf("len: %d", len(l2))
	}
	if l2[0] != 1 {
		t.Errorf("item: %d", l2[0])
	}
	if l2[1] != 2 {
		t.Errorf("item: %d", l2[2])
	}
	if l2[2] != 3 {
		t.Errorf("item: %d", l2[2])
	}
	if l2[3] != 4 {
		t.Errorf("item: %d", l2[3])
	}
}
