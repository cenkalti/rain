package torrent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInvalidTorrentData is test case for reproducing bug:
// https://github.com/cenkalti/rain/v2/issues/23
func TestInvalidTorrentData(t *testing.T) {
	s := newTestSession(t)

	body := "some garbage data"
	r := strings.NewReader(body)
	_, err := s.AddTorrent(r, nil)

	assert.Error(t, err)
}
