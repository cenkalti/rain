package stree

type node struct {
	left, right *node
	// A segment is a interval represented by the node
	segment segment
	// All intervals that overlap with segment
	overlap []interval
}

// Inserts interval into given tree structure
func (n *node) insertInterval(intrvl interval) {
	if n.segment.subsetOf(intrvl.segment) {
		// interval of node is a subset of the specified interval or equal
		if n.overlap == nil {
			n.overlap = make([]interval, 0)
		}
		n.overlap = append(n.overlap, intrvl)
	} else {
		if n.left != nil && n.left.segment.intersectsWith(intrvl.segment) {
			n.left.insertInterval(intrvl)
		}
		if n.right != nil && n.right.segment.intersectsWith(intrvl.segment) {
			n.right.insertInterval(intrvl)
		}
	}
}

// querySingle traverse tree in search of overlaps
func (n node) querySingle(from, to ValueType, result map[ValueType]interval) {
	if n.segment.Disjoint(from, to) {
		return
	}
	for _, pintrvl := range n.overlap {
		result[pintrvl.ID] = pintrvl
	}
	if n.right != nil {
		n.right.querySingle(from, to, result)
	}
	if n.left != nil {
		n.left.querySingle(from, to, result)
	}
}

type interval struct {
	ID ValueType // unique
	segment
}

type segment struct {
	From ValueType
	To   ValueType
}

func (s segment) subsetOf(other segment) bool {
	return other.From <= s.From && other.To >= s.To
}

func (s segment) intersectsWith(other segment) bool {
	return other.From <= s.To && s.From <= other.To ||
		s.From <= other.To && other.From <= s.To
}

// Disjoint returns true if Segment does not overlap with interval
func (s segment) Disjoint(from, to ValueType) bool {
	return from > s.To || to < s.From
}
