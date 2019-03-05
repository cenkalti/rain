package peer

import "strings"

func clientID(id string) string {
	// ID follows BEP 20 convention
	if id[7] == '-' {
		return id[:8]
	}

	// Rain convention
	if strings.HasPrefix(id, "-RN") {
		i := strings.IndexRune(id[1:], '-')
		if i != -1 {
			return id[:i+2]
		}
	}

	return id
}

// asciify replaces non-ascii characters with '_'.
func asciify(id string) string {
	b := []byte(id)
	for i, val := range b {
		if val >= 32 && val < 127 {
			continue
		}
		b[i] = '_'
	}
	return string(b)
}
