package metainfo

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/bencode"
)

func encodeTorrent(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := bencode.EncodeBytes(m)
	require.NoError(t, err)
	return b
}

// baseTorrent is a minimal valid torrent file dict.
func baseTorrent(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"info": bencode.RawMessage(encodeInfo(t, singleFileInfo())),
	}
}

func parse(t *testing.T, m map[string]any) (*MetaInfo, error) {
	t.Helper()
	return New(bytes.NewReader(encodeTorrent(t, m)))
}

func TestNewMinimalTorrent(t *testing.T) {
	mi, err := parse(t, baseTorrent(t))
	require.NoError(t, err)
	assert.Equal(t, "file.txt", mi.Info.Name)
	assert.Empty(t, mi.AnnounceList)
	assert.Empty(t, mi.URLList)
}

func TestNewRejectsBadInput(t *testing.T) {
	full := encodeTorrent(t, baseTorrent(t))
	cases := []struct {
		name string
		in   []byte
	}{
		{"empty", nil},
		{"not bencode", []byte("hello")},
		{"truncated", full[:len(full)/2]},
		{"a list", []byte("le")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := New(bytes.NewReader(c.in))
			assert.Error(t, err)
		})
	}
}

func TestNewRequiresInfoDict(t *testing.T) {
	_, err := New(bytes.NewReader(encodeTorrent(t, map[string]any{
		"announce": "http://tracker/announce",
	})))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no info dict")
}

// A bad info dict has to fail the whole file rather than yield a half-built
// MetaInfo.
func TestNewPropagatesInfoError(t *testing.T) {
	m := singleFileInfo()
	m["piece length"] = 0
	_, err := New(bytes.NewReader(encodeTorrent(t, map[string]any{
		"info": bencode.RawMessage(encodeInfo(t, m)),
	})))
	assert.ErrorIs(t, err, errZeroPieceLength)
}

func TestNewAnnounceList(t *testing.T) {
	m := baseTorrent(t)
	m["announce-list"] = []any{
		[]any{"http://a/announce", "https://b/announce"},
		[]any{"udp://c:1337"},
	}

	mi, err := parse(t, m)
	require.NoError(t, err)
	assert.Equal(t, [][]string{
		{"http://a/announce", "https://b/announce"},
		{"udp://c:1337"},
	}, mi.AnnounceList)
}

// Only http, https and udp are implemented, so anything else is dropped rather
// than carried around as a tracker that can never be contacted.
func TestNewDropsUnsupportedTrackerSchemes(t *testing.T) {
	m := baseTorrent(t)
	m["announce-list"] = []any{
		[]any{"wss://a/announce", "http://b/announce"},
		[]any{"ftp://c/announce"},
		[]any{"not a url"},
	}

	mi, err := parse(t, m)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"http://b/announce"}}, mi.AnnounceList,
		"a tier with nothing supported is dropped entirely")
}

func TestNewFallsBackToAnnounceKey(t *testing.T) {
	m := baseTorrent(t)
	m["announce"] = "http://only/announce"

	mi, err := parse(t, m)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"http://only/announce"}}, mi.AnnounceList)
}

// announce-list takes precedence; the single announce key is only a fallback.
func TestNewAnnounceListWinsOverAnnounce(t *testing.T) {
	m := baseTorrent(t)
	m["announce"] = "http://old/announce"
	m["announce-list"] = []any{[]any{"http://new/announce"}}

	mi, err := parse(t, m)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"http://new/announce"}}, mi.AnnounceList)
}

func TestNewDropsUnsupportedAnnounce(t *testing.T) {
	m := baseTorrent(t)
	m["announce"] = "wss://nope/announce"

	mi, err := parse(t, m)
	require.NoError(t, err)
	assert.Empty(t, mi.AnnounceList)
}

// A malformed announce-list must not sink the torrent; it simply yields no
// trackers, and the torrent can still run on DHT or PEX.
func TestNewToleratesMalformedAnnounceList(t *testing.T) {
	m := baseTorrent(t)
	m["announce-list"] = "not a list of lists"

	mi, err := parse(t, m)
	require.NoError(t, err)
	assert.Empty(t, mi.AnnounceList)
}

// url-list is a string when there is one webseed and a list when there are
// several, so both shapes have to be accepted.
func TestNewURLList(t *testing.T) {
	t.Run("single string", func(t *testing.T) {
		m := baseTorrent(t)
		m["url-list"] = "http://seed/file"
		mi, err := parse(t, m)
		require.NoError(t, err)
		assert.Equal(t, []string{"http://seed/file"}, mi.URLList)
	})

	t.Run("list", func(t *testing.T) {
		m := baseTorrent(t)
		m["url-list"] = []any{"http://a/f", "https://b/f"}
		mi, err := parse(t, m)
		require.NoError(t, err)
		assert.Equal(t, []string{"http://a/f", "https://b/f"}, mi.URLList)
	})

	t.Run("empty list", func(t *testing.T) {
		m := baseTorrent(t)
		m["url-list"] = []any{}
		mi, err := parse(t, m)
		require.NoError(t, err)
		assert.Empty(t, mi.URLList)
	})
}

// Webseeds are fetched over HTTP, so only http and https are usable.
func TestNewDropsUnsupportedWebseedSchemes(t *testing.T) {
	m := baseTorrent(t)
	m["url-list"] = []any{"udp://a/f", "ftp://b/f", "http://c/f"}

	mi, err := parse(t, m)
	require.NoError(t, err)
	assert.Equal(t, []string{"http://c/f"}, mi.URLList)
}

func TestNewToleratesMalformedURLList(t *testing.T) {
	m := baseTorrent(t)
	m["url-list"] = 42

	mi, err := parse(t, m)
	require.NoError(t, err)
	assert.Empty(t, mi.URLList)
}

func TestNewIgnoresUnknownKeys(t *testing.T) {
	m := baseTorrent(t)
	m["comment"] = "hello"
	m["created by"] = "someone"
	m["creation date"] = 1234567890
	m["something we have never seen"] = []any{1, 2, 3}

	mi, err := parse(t, m)
	require.NoError(t, err)
	assert.Equal(t, "file.txt", mi.Info.Name)
}

func TestNewBytesSingleTracker(t *testing.T) {
	info := encodeInfo(t, singleFileInfo())

	b, err := NewBytes(info, [][]string{{"http://a/announce"}}, nil, "")
	require.NoError(t, err)

	var got struct {
		Announce     string     `bencode:"announce"`
		AnnounceList [][]string `bencode:"announce-list"`
	}
	require.NoError(t, bencode.DecodeBytes(b, &got))
	assert.Equal(t, "http://a/announce", got.Announce, "one tracker uses the announce key")
	assert.Empty(t, got.AnnounceList)
}

func TestNewBytesMultipleTrackers(t *testing.T) {
	info := encodeInfo(t, singleFileInfo())
	trackers := [][]string{{"http://a/announce"}, {"http://b/announce"}}

	b, err := NewBytes(info, trackers, nil, "")
	require.NoError(t, err)

	var got struct {
		Announce     string     `bencode:"announce"`
		AnnounceList [][]string `bencode:"announce-list"`
	}
	require.NoError(t, bencode.DecodeBytes(b, &got))
	assert.Empty(t, got.Announce)
	assert.Equal(t, trackers, got.AnnounceList)
}

func TestNewBytesWebseeds(t *testing.T) {
	info := encodeInfo(t, singleFileInfo())

	t.Run("one webseed encodes as a string", func(t *testing.T) {
		b, err := NewBytes(info, nil, []string{"http://seed/f"}, "")
		require.NoError(t, err)
		var got struct {
			URLList bencode.RawMessage `bencode:"url-list"`
		}
		require.NoError(t, bencode.DecodeBytes(b, &got))
		var s string
		require.NoError(t, bencode.DecodeBytes(got.URLList, &s))
		assert.Equal(t, "http://seed/f", s)
	})

	t.Run("several encode as a list", func(t *testing.T) {
		b, err := NewBytes(info, nil, []string{"http://a/f", "http://b/f"}, "")
		require.NoError(t, err)
		var got struct {
			URLList bencode.RawMessage `bencode:"url-list"`
		}
		require.NoError(t, bencode.DecodeBytes(b, &got))
		var l []string
		require.NoError(t, bencode.DecodeBytes(got.URLList, &l))
		assert.Equal(t, []string{"http://a/f", "http://b/f"}, l)
	})
}

func TestNewBytesComment(t *testing.T) {
	info := encodeInfo(t, singleFileInfo())

	b, err := NewBytes(info, nil, nil, "a comment")
	require.NoError(t, err)
	var got struct {
		Comment      string `bencode:"comment"`
		CreationDate int64  `bencode:"creation date"`
	}
	require.NoError(t, bencode.DecodeBytes(b, &got))
	assert.Equal(t, "a comment", got.Comment)
	assert.NotZero(t, got.CreationDate)
}

// What NewBytes writes, New must be able to read, with the info hash intact —
// this is the path a torrent takes when it is re-shared from a magnet link.
func TestNewBytesRoundTripsThroughNew(t *testing.T) {
	info := encodeInfo(t, singleFileInfo())
	original, err := NewInfo(info, true, true)
	require.NoError(t, err)

	b, err := NewBytes(info, [][]string{{"http://a/announce"}, {"udp://b:1337"}},
		[]string{"http://seed/f"}, "re-shared")
	require.NoError(t, err)

	mi, err := New(bytes.NewReader(b))
	require.NoError(t, err)

	assert.Equal(t, original.Hash, mi.Info.Hash, "the info hash must survive a round trip")
	assert.Equal(t, original.Name, mi.Info.Name)
	assert.Equal(t, original.Length, mi.Info.Length)
	assert.Equal(t, [][]string{{"http://a/announce"}, {"udp://b:1337"}}, mi.AnnounceList)
	assert.Equal(t, []string{"http://seed/f"}, mi.URLList)
}

func TestNewBytesRejectsInvalidInfo(t *testing.T) {
	// NewBytes embeds the info dict verbatim, so garbage in means a file that
	// New will reject rather than a silent failure at write time.
	b, err := NewBytes([]byte("not bencode"), nil, nil, "")
	require.NoError(t, err, "NewBytes does not validate the info dict")

	_, err = New(bytes.NewReader(b))
	assert.Error(t, err, "but reading it back must fail")
}
