package peer

import (
	"strings"
)

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
