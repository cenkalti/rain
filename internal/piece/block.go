package piece

// Block is part of a Piece.
type Block struct {
	Piece  *Piece
	Index  uint32 // index in piece
	Begin  uint32 // offset in piece
	Length uint32
}
