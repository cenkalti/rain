package rainrpc_test

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cenkalti/rain/v2/rainrpc"
	"github.com/cenkalti/rain/v2/torrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleTorrent = "../torrent/testdata/sample_torrent.torrent"

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// newTestClient starts a real Session with its JSON-RPC server enabled on a
// free loopback port and returns a rainrpc.Client connected to it.
func newTestClient(t *testing.T) *rainrpc.Client {
	t.Helper()
	tmp := t.TempDir()
	cfg := torrent.DefaultConfig
	cfg.Database = filepath.Join(tmp, "session.db")
	cfg.DataDir = tmp
	cfg.DHTEnabled = false
	cfg.PEXEnabled = false
	cfg.RPCEnabled = true
	cfg.RPCHost = "127.0.0.1"
	cfg.RPCPort = freeTCPPort(t)
	cfg.Host = "127.0.0.1"

	ses, err := torrent.NewSession(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ses.Close()) })

	c := rainrpc.NewClient("http://" + net.JoinHostPort(cfg.RPCHost, strconv.Itoa(cfg.RPCPort)))
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRPCServerVersion(t *testing.T) {
	c := newTestClient(t)
	v, err := c.ServerVersion()
	require.NoError(t, err)
	assert.NotEmpty(t, v)
}

func TestRPCSessionStartsEmpty(t *testing.T) {
	c := newTestClient(t)

	list, err := c.ListTorrents()
	require.NoError(t, err)
	assert.Empty(t, list)

	stats, err := c.GetSessionStats()
	require.NoError(t, err)
	assert.Equal(t, 0, stats.Torrents)
}

// TestRPCAddListRemoveTorrent exercises the add -> inspect -> remove lifecycle
// through the full client -> JSON-RPC -> session handler stack.
func TestRPCAddListRemoveTorrent(t *testing.T) {
	c := newTestClient(t)

	f, err := os.Open(sampleTorrent)
	require.NoError(t, err)
	defer f.Close()

	added, err := c.AddTorrent(f, &rainrpc.AddTorrentOptions{Stopped: true})
	require.NoError(t, err)
	require.NotEmpty(t, added.ID)
	assert.NotEmpty(t, added.InfoHash)

	list, err := c.ListTorrents()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, added.ID, list[0].ID)

	stats, err := c.GetSessionStats()
	require.NoError(t, err)
	assert.Equal(t, 1, stats.Torrents)

	tstats, err := c.GetTorrentStats(added.ID)
	require.NoError(t, err)
	require.NotNil(t, tstats)

	magnet, err := c.GetMagnet(added.ID)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(magnet, "magnet:?xt=urn:btih:"), "got %q", magnet)

	// Stopping an already-stopped torrent still round-trips successfully.
	require.NoError(t, c.StopTorrent(added.ID))

	require.NoError(t, c.RemoveTorrent(added.ID, false))
	list, err = c.ListTorrents()
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestRPCGetStatsUnknownTorrent(t *testing.T) {
	c := newTestClient(t)
	_, err := c.GetTorrentStats("nonexistent")
	require.Error(t, err)
}
