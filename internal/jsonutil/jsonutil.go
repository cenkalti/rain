package jsonutil

import (
	"bytes"
	"sort"
	"strings"

	"github.com/fatih/structs"
	"github.com/hokaccha/go-prettyjson"
)

var formatter *prettyjson.Formatter

func init() {
	formatter = prettyjson.NewFormatter()
	formatter.Indent = 0
	formatter.Newline = ""
}

// MarshalCompactPretty formats the value in a compact JSON form and add color information.
func MarshalCompactPretty(v any) ([]byte, error) {
	var buf bytes.Buffer
	m := structs.Map(v)
	names := structs.Names(v)
	sort.Slice(names, func(i, j int) bool { return strings.Compare(names[i], names[j]) == -1 })
	for _, name := range names {
		val := m[name]
		b, err := formatter.Marshal(val)
		if err != nil {
			return nil, err
		}
		buf.WriteString(name)
		buf.WriteString(": ")
		buf.Write(b)
		buf.WriteRune('\n')
	}
	return buf.Bytes(), nil
}
