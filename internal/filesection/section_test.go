package filesection

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

var data = []string{"asdf", "a", "", "qwerty"}

func TestFiles(t *testing.T) {
	dir, err := os.MkdirTemp("", "partialfile-")
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range data {
		filename := filepath.Join(dir, "file"+strconv.Itoa(i))
		err = os.WriteFile(filename, []byte(s), 0600)
		if err != nil {
			t.Fatal(err)
		}
	}
	osFiles := make([]*os.File, len(data))
	for i := range data {
		filename := filepath.Join(dir, "file"+strconv.Itoa(i))
		osFiles[i], err = os.OpenFile(filename, os.O_RDWR, 0666)
		if err != nil {
			t.Fatal(err)
		}
	}
	files := []FileSection{
		{osFiles[0], 2, 2, "", false},
		{osFiles[1], 0, 1, "", false},
		{osFiles[2], 0, 0, "", false},
		{osFiles[3], 0, 2, "", false},
	}
	pf := Piece(files)

	// test full read
	b := make([]byte, 5)
	n, err := pf.ReadAt(b, 0)
	if err != nil {
		t.Error(err)
	}
	if n != 5 {
		t.Errorf("n == %d", n)
	}
	if string(b) != "dfaqw" {
		t.Errorf("b = %s", string(b))
	}

	// test write
	n, err = pf.Write([]byte("12345"))
	if err != nil {
		t.Error(err)
	}
	if n != 5 {
		t.Errorf("n == %d", n)
	}
	if content(osFiles[0]) != "as12" {
		t.Fail()
	}
	if content(osFiles[1]) != "3" {
		t.Fail()
	}
	if content(osFiles[2]) != "" {
		t.Fail()
	}
	if content(osFiles[3]) != "45erty" {
		t.Fail()
	}
}

func content(f *os.File) string {
	_, _ = f.Seek(0, io.SeekStart)
	fi, _ := f.Stat()
	b := make([]byte, fi.Size())
	_, _ = f.Read(b)
	return string(b)
}
