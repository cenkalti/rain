package jsonutil

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshalCompactPretty(t *testing.T) {
	v := struct {
		Beta  string
		Alpha int
		Gamma bool
	}{Beta: "hello", Alpha: 42, Gamma: true}

	out, err := MarshalCompactPretty(v)
	require.NoError(t, err)
	s := string(out)

	// One "Name: value" line per exported field.
	assert.Equal(t, 3, strings.Count(s, "\n"))
	assert.Contains(t, s, "Alpha: ")
	assert.Contains(t, s, "Beta: ")
	assert.Contains(t, s, "Gamma: ")

	// Fields are emitted in sorted (alphabetical) order.
	assert.Less(t, strings.Index(s, "Alpha"), strings.Index(s, "Beta"))
	assert.Less(t, strings.Index(s, "Beta"), strings.Index(s, "Gamma"))
}
