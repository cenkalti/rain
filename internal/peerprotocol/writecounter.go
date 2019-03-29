package peerprotocol

import "io"

type writerCounter struct {
	io.Writer
	count  int64
	writer io.Writer
}

func newWriterCounter(w io.Writer) *writerCounter {
	return &writerCounter{
		writer: w,
	}
}

func (w *writerCounter) Write(buf []byte) (int, error) {
	n, err := w.writer.Write(buf)
	w.count += int64(n)
	return n, err
}

func (w *writerCounter) Count() int64 {
	return w.count
}
