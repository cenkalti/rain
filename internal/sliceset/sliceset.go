package sliceset

// SliceSet is a set implementation that uses slice internally.
type SliceSet[T any] struct {
	Items []*T
}

// Add the piece to the set.
func (l *SliceSet[T]) Add(pe *T) bool {
	for _, p := range l.Items {
		if p == pe {
			return false
		}
	}
	l.Items = append(l.Items, pe)
	return true
}

// Remove the piece from the set.
func (l *SliceSet[T]) Remove(pe *T) bool {
	for i, p := range l.Items {
		if p == pe {
			l.Items[i] = l.Items[len(l.Items)-1]
			l.Items = l.Items[:len(l.Items)-1]
			return true
		}
	}
	return false
}

// Has returns true if the set contains the piece.
func (l *SliceSet[T]) Has(pe *T) bool {
	for _, p := range l.Items {
		if p == pe {
			return true
		}
	}
	return false
}

// Len returns the number of pieces in the set.
func (l *SliceSet[T]) Len() int {
	return len(l.Items)
}
