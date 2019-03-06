package metainfo

import (
	"strings"

	"github.com/zeebo/bencode"
)

type URLList []string

var _ bencode.Unmarshaler = (*URLList)(nil)

func (u *URLList) UnmarshalBencode(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	var l []string
	var err error
	if b[0] == 'l' {
		err = bencode.DecodeBytes(b, &l)
	} else {
		var s string
		err = bencode.DecodeBytes(b, &s)
		l = append(l, s)
	}
	if err != nil {
		return err
	}
	for i, s := range l {
		if !(strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")) {
			l[i] = l[len(l)-1]
			l = l[:len(l)-1]
		}
	}
	*u = l
	return nil
}
