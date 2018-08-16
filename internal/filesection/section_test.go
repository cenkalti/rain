package filesection

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

var data = []string{"asdf", "a", "", "qwerty"}

func TestFiles(t *testing.T) {
	dir, err := ioutil.TempDir("", "partialfile-")
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range data {
		filename := filepath.Join(dir, "file"+strconv.Itoa(i))
		err = ioutil.WriteFile(filename, []byte(s), 0666)
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
	files := []Section{
		Section{osFiles[0], 2, 2},
		Section{osFiles[1], 0, 1},
		Section{osFiles[2], 0, 0},
		Section{osFiles[3], 0, 2},
	}
	pf := Sections(files)

	// test full read
	b := make([]byte, 5)
	err = pf.ReadFull(b)
	if err != nil {
		t.Error(err)
	}
	if string(b) != "dfaqw" {
		t.Errorf("b = %s", string(b))
	}

	// test partial read
	cases := []struct {
		size   int
		offset int64
		expect string
	}{
		{5, 0, "dfaqw"},
		{4, 0, "dfaq"},
		{3, 0, "dfa"},
		{2, 0, "df"},
		{1, 0, "d"},
		{0, 0, ""},
		{4, 1, "faqw"},
		{3, 2, "aqw"},
		{2, 3, "qw"},
		{1, 4, "w"},
		{0, 5, ""},
		{1, 0, "d"},
		{1, 1, "f"},
		{1, 2, "a"},
		{1, 3, "q"},
		{1, 4, "w"},
	}
	for _, c := range cases {
		b = make([]byte, c.size)
		err = pf.ReadAt(b, c.offset)
		if err != nil {
			t.Error(err)
		}
		if string(b) != c.expect {
			t.Errorf("b = %s", string(b))
		}
	}

	// test write
	n, err := pf.Write([]byte("12345"))
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
	f.Seek(0, io.SeekStart)
	fi, _ := f.Stat()
	b := make([]byte, fi.Size())
	f.Read(b)
	return string(b)
}
