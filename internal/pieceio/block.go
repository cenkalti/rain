package pieceio

const BlockSize = 16 * 1024

// Block is part of a Piece that is specified in peerprotocol.Request messages.
type Block struct {
	Index  uint32 // index in piece
	Begin  uint32 // offset in piece
	Length uint32 // always equal to BlockSize except the last block of a piece.
}

type Blocks []Block

func newBlocks(length uint32) Blocks {
	div, mod := divMod32(length, BlockSize)
	numBlocks := div
	if mod != 0 {
		numBlocks++
	}
	blocks := make([]Block, numBlocks)
	for j := uint32(0); j < div; j++ {
		blocks[j] = Block{
			Index:  j,
			Begin:  j * BlockSize,
			Length: BlockSize,
		}
	}
	if mod != 0 {
		blocks[numBlocks-1] = Block{
			Index:  numBlocks - 1,
			Begin:  (numBlocks - 1) * BlockSize,
			Length: mod,
		}
	}
	return blocks
}

func (a Blocks) Find(begin, length uint32) *Block {
	idx, mod := divMod32(begin, BlockSize)
	if mod != 0 {
		return nil
	}
	if idx >= uint32(len(a)) {
		return nil
	}
	b := &a[idx]
	if b.Length != length {
		return nil
	}
	return b
}
