package filestorage

import "os"

type File struct {
	*os.File
}

func (f *File) Write(b []byte) (n int, err error) {
	n, err = f.File.Write(b)
	if err != nil {
		return
	}
	return n, f.File.Sync()
}
