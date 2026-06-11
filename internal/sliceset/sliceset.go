package sliceset

import "slices"

// SliceSet is a set implementation that uses slice internally.
type SliceSet[T any] struct {
	Items []*T
}

// Add the piece to the set.
func (l *SliceSet[T]) Add(pe *T) bool {
	if slices.Contains(l.Items, pe) {
		return false
	}
	l.Items = append(l.Items, pe)
	return true
}

// Remove the piece from the set.
func (l *SliceSet[T]) Remove(pe *T) bool {
	i := slices.Index(l.Items, pe)
	if i == -1 {
		return false
	}
	// Swap-with-last removal: order is not preserved.
	l.Items[i] = l.Items[len(l.Items)-1]
	l.Items = l.Items[:len(l.Items)-1]
	return true
}

// Has returns true if the set contains the piece.
func (l *SliceSet[T]) Has(pe *T) bool {
	return slices.Contains(l.Items, pe)
}

// Len returns the number of pieces in the set.
func (l *SliceSet[T]) Len() int {
	return len(l.Items)
}
