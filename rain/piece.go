package rain

type piece struct {
	index  int
	blocks []*block
}

func newPiece(i int) *piece {
	p := &piece{
		index: i,
		// blocks: make([]*block, )
	}
	return p
}
