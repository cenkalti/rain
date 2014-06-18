package rain

type block struct {
	index  int32 // block index in piece
	length int32
	files  partialFiles // the place to write downloaded bytes
}
