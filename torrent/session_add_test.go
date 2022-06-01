package torrent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInvalidTorrentData is test case for reproducing bug:
// https://github.com/cenkalti/rain/issues/23
func TestInvalidTorrentData(t *testing.T) {
	s, closeSession := newTestSession(t)
	defer closeSession()

	body := "some garbage data"
	r := strings.NewReader(body)
	_, err := s.AddTorrent(r, nil)

	assert.Error(t, err)
}
