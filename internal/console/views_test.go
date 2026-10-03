package console

import (
	"testing"

	"github.com/cenkalti/rain/v2/internal/rpctypes"
	"github.com/stretchr/testify/assert"
)

func TestGetHeader(t *testing.T) {
	cases := []struct {
		columns []string
		want    string
	}{
		{nil, ""},
		{[]string{"#"}, "  #"},
		{[]string{"Name"}, "Name"},
		{[]string{"Port"}, " Port"},
		{[]string{"Status"}, "Status     "},
		{[]string{"Speed"}, "   Speed"},
		{[]string{"ETA"}, "     ETA"},
		{[]string{"Progress"}, "Progress"},
		{[]string{"Ratio"}, "Ratio"},
		{[]string{"Size"}, "    Size"},
		{[]string{"#", "Name"}, "  # Name"},
		{[]string{"Name", "Ratio", "Size"}, "Name Ratio     Size"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, getHeader(c.columns), "getHeader(%v)", c.columns)
	}
}

func TestGetHeaderFixedWidths(t *testing.T) {
	// Columns with a fixed width must keep it, or rows stop lining up.
	for col, width := range map[string]int{
		"#": 3, "ID": 22, "InfoHash": 40, "Port": 5,
		"Status": 11, "Speed": 8, "ETA": 8, "Progress": 8,
		"Ratio": 5, "Size": 8,
	} {
		assert.Len(t, getHeader([]string{col}), width, "header width of %q", col)
	}
}

func TestGetHeaderPanicsOnUnknownColumn(t *testing.T) {
	assert.PanicsWithValue(t, "unsupported column Bogus", func() {
		getHeader([]string{"Bogus"})
	})
}

func TestGetRowWithoutStats(t *testing.T) {
	tor := Torrent{Torrent: rpctypes.Torrent{
		ID:       "abc",
		Name:     "ubuntu.iso",
		InfoHash: "0123456789abcdef0123456789abcdef01234567",
		Port:     6881,
	}}
	cases := []struct {
		column string
		want   string
	}{
		{"#", "  1\n"},
		{"ID", "abc\n"},
		{"Name", "ubuntu.iso\n"},
		{"InfoHash", "0123456789abcdef0123456789abcdef01234567\n"},
		{"Port", " 6881\n"},
		// Every stats-derived column renders as padding when Stats is nil.
		{"Status", "           \n"},
		{"Speed", "        \n"},
		{"ETA", "        \n"},
		{"Progress", "        \n"},
		{"Ratio", "     \n"},
		{"Size", "        \n"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, getRow([]string{c.column}, tor, 0), "getRow(%q) with nil Stats", c.column)
	}
}

func TestGetRowIndexIsOneBased(t *testing.T) {
	assert.Equal(t, "  1\n", getRow([]string{"#"}, Torrent{}, 0))
	assert.Equal(t, " 42\n", getRow([]string{"#"}, Torrent{}, 41))
	assert.Equal(t, "100\n", getRow([]string{"#"}, Torrent{}, 99))
}

func TestGetRowWithStats(t *testing.T) {
	withStats := func(f func(s *rpctypes.Stats)) Torrent {
		var s rpctypes.Stats
		f(&s)
		return Torrent{Stats: &s}
	}
	cases := []struct {
		name   string
		column string
		tor    Torrent
		want   string
	}{
		{
			"status is padded",
			"Status",
			withStats(func(s *rpctypes.Stats) { s.Status = "Seeding" }),
			"Seeding    \n",
		},
		{
			"metadata status is shortened to fit",
			"Status",
			withStats(func(s *rpctypes.Stats) { s.Status = "Downloading Metadata" }),
			"Downloading\n",
		},
		{
			"speed shows download while downloading",
			"Speed",
			withStats(func(s *rpctypes.Stats) {
				s.Status = "Downloading"
				s.Speed.Download = 2048
				s.Speed.Upload = 9999999
			}),
			"     2 K\n",
		},
		{
			"speed shows upload while seeding",
			"Speed",
			withStats(func(s *rpctypes.Stats) {
				s.Status = "Seeding"
				s.Speed.Download = 9999999
				s.Speed.Upload = 4096
			}),
			"     4 K\n",
		},
		{
			"eta",
			"ETA",
			withStats(func(s *rpctypes.Stats) { s.ETA = 90 }),
			"   1m30s\n",
		},
		{
			"unknown eta is blank",
			"ETA",
			withStats(func(s *rpctypes.Stats) { s.ETA = -1 }),
			"        \n",
		},
		{
			"progress",
			"Progress",
			withStats(func(s *rpctypes.Stats) { s.Pieces.Have = 1; s.Pieces.Total = 4 }),
			"      25\n",
		},
		{
			"ratio",
			"Ratio",
			withStats(func(s *rpctypes.Stats) { s.Bytes.Uploaded = 50; s.Bytes.Downloaded = 100 }),
			" 0.50\n",
		},
		{
			"size in whole mib",
			"Size",
			withStats(func(s *rpctypes.Stats) { s.Bytes.Total = 5 << 20 }),
			"     5 M\n",
		},
		{
			"size truncates below a mib",
			"Size",
			withStats(func(s *rpctypes.Stats) { s.Bytes.Total = 1023 }),
			"     0 M\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, getRow([]string{c.column}, c.tor, 0))
		})
	}
}

func TestGetRowSeparatesColumnsWithSingleSpace(t *testing.T) {
	tor := Torrent{Torrent: rpctypes.Torrent{ID: "abc", Name: "x", Port: 1}}
	assert.Equal(t, "  1 abc x     1\n", getRow([]string{"#", "ID", "Name", "Port"}, tor, 0))
}

func TestGetRowPanicsOnUnknownColumn(t *testing.T) {
	assert.PanicsWithValue(t, "unsupported column Bogus", func() {
		getRow([]string{"Bogus"}, Torrent{}, 0)
	})
}
