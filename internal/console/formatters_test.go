package console

import (
	"slices"
	"strings"
	"testing"

	"github.com/cenkalti/rain/v2/internal/rpctypes"
	"github.com/stretchr/testify/assert"
)

func TestIsURI(t *testing.T) {
	cases := []struct {
		arg  string
		want bool
	}{
		{"magnet:?xt=urn:btih:0000", true},
		{"http://example.com/a.torrent", true},
		{"https://example.com/a.torrent", true},
		{"/path/to/a.torrent", false},
		{"a.torrent", false},
		{"ftp://example.com/a.torrent", false},
		{"MAGNET:?xt=urn:btih:0000", false},
		{"", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, isURI(c.arg), "isURI(%q)", c.arg)
	}
}

func TestFlags(t *testing.T) {
	cases := []struct {
		name string
		peer rpctypes.Peer
		want string
	}{
		{"zero value", rpctypes.Peer{}, "K?    "},
		{"interested, peer choking", rpctypes.Peer{ClientInterested: true, PeerChoking: true}, "d?    "},
		{"interested, unchoked", rpctypes.Peer{ClientInterested: true}, "D?    "},
		{"uninterested, peer choking", rpctypes.Peer{PeerChoking: true}, " ?    "},
		{"peer interested, we choke", rpctypes.Peer{PeerInterested: true, ClientChoking: true}, "Ku    "},
		{"peer interested, unchoked", rpctypes.Peer{PeerInterested: true}, "KU    "},
		{"peer uninterested, we choke", rpctypes.Peer{ClientChoking: true}, "K     "},
		{"optimistic", rpctypes.Peer{OptimisticUnchoked: true}, "K?O   "},
		{"snubbed", rpctypes.Peer{Snubbed: true}, "K? S  "},
		{"source dht", rpctypes.Peer{Source: "DHT"}, "K?  H "},
		{"source pex", rpctypes.Peer{Source: "PEX"}, "K?  X "},
		{"source incoming", rpctypes.Peer{Source: "INCOMING"}, "K?  I "},
		{"source manual", rpctypes.Peer{Source: "MANUAL"}, "K?  M "},
		{"source unknown", rpctypes.Peer{Source: "SOMETHING"}, "K?    "},
		{"encrypted stream", rpctypes.Peer{EncryptedStream: true}, "K?   E"},
		{"encrypted handshake", rpctypes.Peer{EncryptedHandshake: true}, "K?   e"},
		{"stream beats handshake", rpctypes.Peer{EncryptedStream: true, EncryptedHandshake: true}, "K?   E"},
		{
			"all set",
			rpctypes.Peer{
				ClientInterested:   true,
				PeerInterested:     true,
				OptimisticUnchoked: true,
				Snubbed:            true,
				Source:             "DHT",
				EncryptedStream:    true,
			},
			"DUOSHE",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := flags(c.peer)
			assert.Equal(t, c.want, got)
			assert.Len(t, got, 6, "flags must always be 6 columns wide")
		})
	}
}

func TestGetProgress(t *testing.T) {
	stats := func(status string, checked, have, total uint32, allocated, bytesTotal int64) *rpctypes.Stats {
		var s rpctypes.Stats
		s.Status = status
		s.Pieces.Checked = checked
		s.Pieces.Have = have
		s.Pieces.Total = total
		s.Bytes.Allocated = allocated
		s.Bytes.Total = bytesTotal
		return &s
	}
	cases := []struct {
		name  string
		stats *rpctypes.Stats
		want  int
	}{
		{"no pieces", stats("Downloading", 0, 0, 0, 0, 0), 0},
		{"verifying uses checked", stats("Verifying", 25, 50, 100, 0, 0), 25},
		{"allocating uses bytes", stats("Allocating", 0, 0, 100, 512, 1024), 50},
		{"downloading uses have", stats("Downloading", 25, 50, 100, 0, 0), 50},
		{"seeding uses have", stats("Seeding", 0, 100, 100, 0, 0), 100},
		{"truncates toward zero", stats("Downloading", 0, 1, 3, 0, 0), 33},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, getProgress(c.stats))
		})
	}
}

func TestGetRatio(t *testing.T) {
	ratio := func(up, down int64) float64 {
		var s rpctypes.Stats
		s.Bytes.Uploaded = up
		s.Bytes.Downloaded = down
		return getRatio(&s)
	}
	assert.Equal(t, 0.0, ratio(100, 0), "zero downloaded must not divide")
	assert.Equal(t, 0.0, ratio(0, 0))
	assert.Equal(t, 1.0, ratio(100, 100))
	assert.Equal(t, 0.5, ratio(50, 100))
	assert.Equal(t, 2.0, ratio(200, 100))
}

func TestGetSize(t *testing.T) {
	size := func(total int64) string {
		var s rpctypes.Stats
		s.Bytes.Total = total
		return getSize(&s)
	}
	assert.Equal(t, "0 bytes", size(0))
	assert.Equal(t, "1023 bytes", size(1023))
	assert.Equal(t, "1 KiB", size(1024))
	assert.Equal(t, "1023 KiB", size(1<<20-1024))
	assert.Equal(t, "1 MiB", size(1<<20))
	assert.Equal(t, "1024 MiB", size(1<<30))
}

func TestGetSpeeds(t *testing.T) {
	var s rpctypes.Stats
	s.Speed.Download = 2048
	s.Speed.Upload = 5120
	assert.Equal(t, "2 KiB/s", getDownloadSpeed(&s))
	assert.Equal(t, "5 KiB/s", getUploadSpeed(&s))

	var zero rpctypes.Stats
	assert.Equal(t, "0 KiB/s", getDownloadSpeed(&zero))
	assert.Equal(t, "0 KiB/s", getUploadSpeed(&zero))
}

func TestGetETA(t *testing.T) {
	eta := func(v int) string {
		var s rpctypes.Stats
		s.ETA = v
		return getETA(&s)
	}
	assert.Equal(t, "", eta(-1), "unknown ETA renders empty")
	assert.Equal(t, "0s", eta(0))
	assert.Equal(t, "1m30s", eta(90))
	assert.Equal(t, "1h0m0s", eta(3600))
}

func TestFormatStats(t *testing.T) {
	var s rpctypes.Stats
	s.Name = "ubuntu.iso"
	s.Private = true
	s.Status = "Downloading"
	s.Pieces.Have = 1
	s.Pieces.Total = 4
	s.Bytes.Total = 2048
	s.Bytes.Uploaded = 512
	s.Bytes.Downloaded = 1024
	s.Peers.Incoming = 3
	s.Peers.Outgoing = 7
	s.Speed.Download = 1024
	s.Speed.Upload = 2048
	s.ETA = 60

	var sb strings.Builder
	FormatStats(&s, &sb)

	assert.Equal(t, strings.Join([]string{
		"Name: ubuntu.iso",
		"Private: true",
		"Status: Downloading",
		"Progress: 25%",
		"Ratio: 0.50",
		"Size: 2 KiB",
		"Peers: 3 in / 7 out",
		"Download speed:     1 KiB/s",
		"Upload speed:       2 KiB/s",
		"ETA: 1m0s",
		"",
	}, "\n"), sb.String())
}

func TestFormatStatsAppendsErrorToStoppedStatus(t *testing.T) {
	var s rpctypes.Stats
	s.Status = "Stopped"
	s.Error = "disk full"

	var sb strings.Builder
	FormatStats(&s, &sb)
	assert.Contains(t, sb.String(), "Status: Stopped: disk full\n")

	// An error on any other status is not surfaced by FormatStats.
	var running rpctypes.Stats
	running.Status = "Downloading"
	running.Error = "disk full"

	sb.Reset()
	FormatStats(&running, &sb)
	assert.Contains(t, sb.String(), "Status: Downloading\n")
	assert.NotContains(t, sb.String(), "disk full")
}

func TestFormatSessionStats(t *testing.T) {
	s := rpctypes.SessionStats{
		Uptime:                90,
		Torrents:              2,
		Peers:                 11,
		BlockListRules:        100,
		BlockListRecency:      3600,
		ReadsPerSecond:        4,
		SpeedRead:             2048,
		ReadsActive:           1,
		ReadsPending:          2,
		WritesPerSecond:       5,
		SpeedWrite:            4096,
		WritesActive:          3,
		WritesPending:         4,
		ReadCacheObjects:      6,
		ReadCacheSize:         2 << 20,
		ReadCacheUtilization:  42,
		WriteCacheObjects:     7,
		WriteCacheSize:        3 << 20,
		WriteCachePendingKeys: 8,
		SpeedDownload:         1024,
		SpeedUpload:           3072,
		BytesDownloaded:       5 << 20,
		BytesUploaded:         6 << 20,
		BytesRead:             7 << 20,
		BytesWritten:          8 << 20,
	}

	var sb strings.Builder
	FormatSessionStats(&s, &sb)

	assert.Equal(t, strings.Join([]string{
		"Torrents: 2, Peers: 11, Uptime: 1m30s",
		"BlocklistRules: 100, Updated: 1h0m0s ago",
		"Reads: 4/s, 2KB/s, Active: 1, Pending: 2",
		"Writes: 5/s, 4KB/s, Active: 3, Pending: 4",
		"ReadCache Objects: 6, Size: 2MB, Utilization: 42%",
		"WriteCache Objects: 7, Size: 3MB, PendingKeys: 8",
		"DownloadSpeed: 1KB/s, UploadSpeed: 3KB/s",
		"BytesDownloaded: 5MB, BytesUploaded: 6MB",
		"BytesRead: 7MB, BytesWritten: 8MB",
		"",
	}, "\n"), sb.String())
}

func TestColumnsNeedStats(t *testing.T) {
	cases := []struct {
		name    string
		columns []string
		want    bool
	}{
		{"nil", nil, false},
		{"empty", []string{}, false},
		{"default columns", []string{"#", "ID", "Name"}, false},
		{"every torrent-only column", columnsFromTorrent, false},
		{"status alone", []string{"Status"}, true},
		{"progress alone", []string{"Progress"}, true},
		{"one stats column among cheap ones", []string{"#", "ID", "Name", "Ratio"}, true},
		{"unknown column is assumed to need stats", []string{"Bogus"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, columnsNeedStats(c.columns))
		})
	}
}

// Every column getRow can render must be classified: either it is listed in
// columnsFromTorrent, or asking for it alone must request stats. Otherwise a
// new column silently renders blank.
func TestColumnsNeedStatsCoversEveryColumn(t *testing.T) {
	all := []string{"#", "ID", "Name", "InfoHash", "Port", "Status", "Speed",
		"ETA", "Progress", "Ratio", "Size"}
	for _, col := range all {
		// getHeader panics on a column it does not know, so this also asserts
		// the list above stays in step with the switch in views.go.
		assert.NotPanics(t, func() { getHeader([]string{col}) }, "column %q", col)

		needs := columnsNeedStats([]string{col})
		if slices.Contains(columnsFromTorrent, col) {
			assert.False(t, needs, "column %q is torrent-only, must not need stats", col)
		} else {
			assert.True(t, needs, "column %q is stats-derived, must need stats", col)
		}
	}
}
