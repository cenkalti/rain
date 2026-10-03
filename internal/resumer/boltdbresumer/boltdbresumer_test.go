package boltdbresumer

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"
)

var testBucket = []byte("torrents")

func newTestDB(t *testing.T) *bbolt.DB {
	t.Helper()
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "resume.db"), 0600, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func newTestResumer(t *testing.T) *Resumer {
	t.Helper()
	r, err := New(newTestDB(t), testBucket)
	require.NoError(t, err)
	return r
}

// fullSpec is populated in every field so a round-trip exercises each key.
// AddedAt is whole-second UTC because the on-disk format is RFC3339, which
// cannot carry anything finer — see TestAddedAtLosesSubSecondPrecision.
func fullSpec() *Spec {
	return &Spec{
		InfoHash:          []byte("01234567890123456789"),
		Port:              6881,
		Name:              "ubuntu.iso",
		Trackers:          [][]string{{"http://a/announce", "http://b/announce"}, {"udp://c:1337"}},
		URLList:           []string{"http://seed/file"},
		FixedPeers:        []string{"10.0.0.1:6881"},
		Info:              []byte("d4:infoe"),
		Bitfield:          []byte{0xff, 0x0f},
		AddedAt:           time.Date(2026, 3, 1, 12, 30, 45, 0, time.UTC),
		BytesDownloaded:   1 << 30,
		BytesUploaded:     1 << 20,
		BytesWasted:       4096,
		SeededFor:         90 * time.Minute,
		Started:           true,
		StopAfterDownload: true,
		StopAfterMetadata: true,
		CompleteCmdRun:    true,
		Sequential:        true,
		Version:           LatestVersion,
	}
}

// The key names and the version number are the on-disk contract. Renaming a key
// or bumping LatestVersion without a migration silently orphans the resume data
// of every existing session, so both are pinned here deliberately: if this test
// fails, the change needs a migration, not a new expectation.
func TestOnDiskKeysAreStable(t *testing.T) {
	assert.Equal(t, 3, LatestVersion, "bumping LatestVersion requires a migration path in Read")

	assert.Equal(t, map[string]string{
		"InfoHash":          "info_hash",
		"Port":              "port",
		"Name":              "name",
		"Trackers":          "trackers",
		"URLList":           "url_list",
		"FixedPeers":        "fixed_peers",
		"Dest":              "dest",
		"Info":              "info",
		"Bitfield":          "bitfield",
		"AddedAt":           "added_at",
		"BytesDownloaded":   "bytes_downloaded",
		"BytesUploaded":     "bytes_uploaded",
		"BytesWasted":       "bytes_wasted",
		"SeededFor":         "seeded_for",
		"Started":           "started",
		"StopAfterDownload": "stop_after_download",
		"StopAfterMetadata": "stop_after_metadata",
		"CompleteCmdRun":    "complete_cmd_run",
		"Sequential":        "sequential",
		"Version":           "version",
	}, map[string]string{
		"InfoHash":          string(Keys.InfoHash),
		"Port":              string(Keys.Port),
		"Name":              string(Keys.Name),
		"Trackers":          string(Keys.Trackers),
		"URLList":           string(Keys.URLList),
		"FixedPeers":        string(Keys.FixedPeers),
		"Dest":              string(Keys.Dest),
		"Info":              string(Keys.Info),
		"Bitfield":          string(Keys.Bitfield),
		"AddedAt":           string(Keys.AddedAt),
		"BytesDownloaded":   string(Keys.BytesDownloaded),
		"BytesUploaded":     string(Keys.BytesUploaded),
		"BytesWasted":       string(Keys.BytesWasted),
		"SeededFor":         string(Keys.SeededFor),
		"Started":           string(Keys.Started),
		"StopAfterDownload": string(Keys.StopAfterDownload),
		"StopAfterMetadata": string(Keys.StopAfterMetadata),
		"CompleteCmdRun":    string(Keys.CompleteCmdRun),
		"Sequential":        string(Keys.Sequential),
		"Version":           string(Keys.Version),
	})
}

func TestNewCreatesBucketAndIsIdempotent(t *testing.T) {
	db := newTestDB(t)

	_, err := New(db, testBucket)
	require.NoError(t, err)

	// Calling New again on the same database must not fail or lose data.
	r, err := New(db, testBucket)
	require.NoError(t, err)
	require.NoError(t, r.Write("t1", fullSpec()))

	_, err = New(db, testBucket)
	require.NoError(t, err)

	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.Equal(t, "ubuntu.iso", got.Name)
}

func TestWriteReadRoundTrip(t *testing.T) {
	r := newTestResumer(t)
	want := fullSpec()
	require.NoError(t, r.Write("t1", want))

	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestWriteReadZeroSpec(t *testing.T) {
	r := newTestResumer(t)
	// InfoHash must be present for Read to accept the record, but everything
	// else may legitimately be empty on a freshly added magnet.
	require.NoError(t, r.Write("t1", &Spec{InfoHash: []byte("01234567890123456789")}))

	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.Equal(t, []byte("01234567890123456789"), got.InfoHash)
	assert.Zero(t, got.Port)
	assert.Empty(t, got.Name)
	assert.Nil(t, got.Trackers)
	assert.True(t, got.AddedAt.IsZero())
	assert.Equal(t, LatestVersion, got.Version, "an unversioned write gets the latest version")
}

func TestWritePreservesExplicitVersion(t *testing.T) {
	r := newTestResumer(t)

	s := fullSpec()
	s.Version = 2
	require.NoError(t, r.Write("t1", s))
	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.Equal(t, 2, got.Version, "an explicit version must not be overwritten")

	s.Version = 0
	require.NoError(t, r.Write("t2", s))
	got, err = r.Read("t2")
	require.NoError(t, err)
	assert.Equal(t, LatestVersion, got.Version, "version 0 means unset, so use the latest")
}

// Records written before the version key existed must read back as version 1,
// which is what tells the torrent layer they need the older interpretation.
func TestRecordWithoutVersionKeyReadsAsVersion1(t *testing.T) {
	r := newTestResumer(t)
	require.NoError(t, r.Write("t1", fullSpec()))

	require.NoError(t, r.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(testBucket).Bucket([]byte("t1")).Delete(Keys.Version)
	}))

	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.Equal(t, 1, got.Version)
}

// Trackers were once stored as []string and are now [][]string. Read migrates
// the old shape and writes it back so the conversion happens once.
func TestReadMigratesFlatTrackerList(t *testing.T) {
	r := newTestResumer(t)
	require.NoError(t, r.Write("t1", fullSpec()))

	old, err := json.Marshal([]string{"http://a/announce", "http://b/announce"})
	require.NoError(t, err)
	require.NoError(t, r.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(testBucket).Bucket([]byte("t1")).Put(Keys.Trackers, old)
	}))

	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"http://a/announce"}, {"http://b/announce"}}, got.Trackers,
		"each old flat entry becomes its own tier")

	// The migration must be persisted, not redone on every read.
	var stored []byte
	require.NoError(t, r.db.View(func(tx *bbolt.Tx) error {
		stored = append(stored, tx.Bucket(testBucket).Bucket([]byte("t1")).Get(Keys.Trackers)...)
		return nil
	}))
	assert.JSONEq(t, `[["http://a/announce"],["http://b/announce"]]`, string(stored))
}

func TestReadMissingTorrent(t *testing.T) {
	r := newTestResumer(t)
	spec, err := r.Read("nope")
	require.Error(t, err)
	assert.Nil(t, spec)
	assert.Contains(t, err.Error(), "bucket not found")
}

func TestReadRecordWithoutInfoHash(t *testing.T) {
	r := newTestResumer(t)
	require.NoError(t, r.Write("t1", fullSpec()))
	require.NoError(t, r.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(testBucket).Bucket([]byte("t1")).Delete(Keys.InfoHash)
	}))

	_, err := r.Read("t1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

// A corrupt value in any parsed field must surface as an error rather than a
// panic or a silently wrong spec.
func TestReadRejectsCorruptValues(t *testing.T) {
	cases := []struct {
		name string
		key  []byte
		val  string
	}{
		{"port", Keys.Port, "not-a-number"},
		{"port empty", Keys.Port, ""},
		{"added_at", Keys.AddedAt, "yesterday"},
		{"bytes_downloaded", Keys.BytesDownloaded, "lots"},
		{"bytes_uploaded", Keys.BytesUploaded, "lots"},
		{"bytes_wasted", Keys.BytesWasted, "lots"},
		{"seeded_for", Keys.SeededFor, "forever"},
		{"started", Keys.Started, "maybe"},
		{"stop_after_download", Keys.StopAfterDownload, "maybe"},
		{"stop_after_metadata", Keys.StopAfterMetadata, "maybe"},
		{"complete_cmd_run", Keys.CompleteCmdRun, "maybe"},
		{"sequential", Keys.Sequential, "maybe"},
		{"version", Keys.Version, "three"},
		{"trackers", Keys.Trackers, "{not json"},
		{"url_list", Keys.URLList, "{not json"},
		{"fixed_peers", Keys.FixedPeers, "{not json"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newTestResumer(t)
			require.NoError(t, r.Write("t1", fullSpec()))
			require.NoError(t, r.db.Update(func(tx *bbolt.Tx) error {
				return tx.Bucket(testBucket).Bucket([]byte("t1")).Put(c.key, []byte(c.val))
			}))

			_, err := r.Read("t1")
			assert.Error(t, err, "corrupt %s must be reported", c.name)
		})
	}
}

func TestWriteInfoAndBitfield(t *testing.T) {
	r := newTestResumer(t)
	require.NoError(t, r.Write("t1", fullSpec()))

	require.NoError(t, r.WriteInfo("t1", []byte("new-info")))
	require.NoError(t, r.WriteBitfield("t1", []byte{0x01, 0x02}))

	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.Equal(t, []byte("new-info"), got.Info)
	assert.Equal(t, []byte{0x01, 0x02}, got.Bitfield)
}

func TestWriteStarted(t *testing.T) {
	r := newTestResumer(t)
	require.NoError(t, r.Write("t1", fullSpec()))

	require.NoError(t, r.WriteStarted("t1", false))
	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.False(t, got.Started)

	require.NoError(t, r.WriteStarted("t1", true))
	got, err = r.Read("t1")
	require.NoError(t, err)
	assert.True(t, got.Started)
}

func TestHandleStopAfterDownload(t *testing.T) {
	r := newTestResumer(t)
	require.NoError(t, r.Write("t1", fullSpec()))

	require.NoError(t, r.HandleStopAfterDownload("t1"))

	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.False(t, got.Started, "stopping after download clears the start flag")
	assert.False(t, got.StopAfterDownload, "and the request itself, so it fires once")
	assert.True(t, got.StopAfterMetadata, "the metadata flag is left alone")
}

func TestHandleStopAfterMetadata(t *testing.T) {
	r := newTestResumer(t)
	require.NoError(t, r.Write("t1", fullSpec()))

	require.NoError(t, r.HandleStopAfterMetadata("t1"))

	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.False(t, got.Started)
	assert.False(t, got.StopAfterMetadata)
	assert.True(t, got.StopAfterDownload, "the download flag is left alone")
}

func TestWriteCompleteCmdRun(t *testing.T) {
	r := newTestResumer(t)
	s := fullSpec()
	s.CompleteCmdRun = false
	require.NoError(t, r.Write("t1", s))

	require.NoError(t, r.WriteCompleteCmdRun("t1"))

	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.True(t, got.CompleteCmdRun)
}

// Every partial writer goes through update, which no-ops when the torrent is
// absent. Removing a torrent races these calls, so they must not error or
// recreate the record.
func TestPartialWritesOnMissingTorrentAreNoOps(t *testing.T) {
	r := newTestResumer(t)

	assert.NoError(t, r.WriteInfo("gone", []byte("x")))
	assert.NoError(t, r.WriteBitfield("gone", []byte("x")))
	assert.NoError(t, r.WriteStarted("gone", true))
	assert.NoError(t, r.HandleStopAfterDownload("gone"))
	assert.NoError(t, r.HandleStopAfterMetadata("gone"))
	assert.NoError(t, r.WriteCompleteCmdRun("gone"))

	_, err := r.Read("gone")
	assert.Error(t, err, "a no-op write must not bring the torrent back")
}

func TestWriteOverwritesExistingRecord(t *testing.T) {
	r := newTestResumer(t)
	require.NoError(t, r.Write("t1", fullSpec()))

	second := fullSpec()
	second.Name = "renamed"
	second.Port = 7000
	second.Started = false
	require.NoError(t, r.Write("t1", second))

	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.Equal(t, "renamed", got.Name)
	assert.Equal(t, 7000, got.Port)
	assert.False(t, got.Started)
}

func TestTorrentsAreIsolated(t *testing.T) {
	r := newTestResumer(t)
	a, b := fullSpec(), fullSpec()
	a.Name, b.Name = "a", "b"
	a.Port, b.Port = 1, 2
	require.NoError(t, r.Write("ta", a))
	require.NoError(t, r.Write("tb", b))

	require.NoError(t, r.WriteStarted("ta", false))

	gotA, err := r.Read("ta")
	require.NoError(t, err)
	gotB, err := r.Read("tb")
	require.NoError(t, err)

	assert.Equal(t, "a", gotA.Name)
	assert.False(t, gotA.Started)
	assert.Equal(t, "b", gotB.Name)
	assert.True(t, gotB.Started, "writing one torrent must not touch another")
}

// AddedAt is stored as RFC3339, which has second resolution. Anything finer is
// dropped on the way to disk, so callers cannot rely on the exact instant
// coming back.
func TestAddedAtLosesSubSecondPrecision(t *testing.T) {
	r := newTestResumer(t)
	s := fullSpec()
	s.AddedAt = time.Date(2026, 3, 1, 12, 30, 45, 123456789, time.UTC)
	require.NoError(t, r.Write("t1", s))

	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.True(t, got.AddedAt.Equal(time.Date(2026, 3, 1, 12, 30, 45, 0, time.UTC)),
		"sub-second precision is truncated, got %s", got.AddedAt)
}

func TestReadDoesNotAliasDatabaseMemory(t *testing.T) {
	r := newTestResumer(t)
	require.NoError(t, r.Write("t1", fullSpec()))

	got, err := r.Read("t1")
	require.NoError(t, err)

	// Read copies InfoHash, Info and Bitfield out of the mmap. Mutating the
	// returned spec must not corrupt the database.
	got.InfoHash[0] ^= 0xff
	got.Info[0] ^= 0xff
	got.Bitfield[0] ^= 0xff

	again, err := r.Read("t1")
	require.NoError(t, err)
	assert.Equal(t, fullSpec().InfoHash, again.InfoHash)
	assert.Equal(t, fullSpec().Info, again.Info)
	assert.Equal(t, fullSpec().Bitfield, again.Bitfield)
}

func TestPortRoundTripsAsDecimalString(t *testing.T) {
	// The port is stored as text, so confirm the encoding rather than assuming.
	r := newTestResumer(t)
	s := fullSpec()
	s.Port = 65535
	require.NoError(t, r.Write("t1", s))

	var raw string
	require.NoError(t, r.db.View(func(tx *bbolt.Tx) error {
		raw = string(tx.Bucket(testBucket).Bucket([]byte("t1")).Get(Keys.Port))
		return nil
	}))
	assert.Equal(t, strconv.Itoa(65535), raw)

	got, err := r.Read("t1")
	require.NoError(t, err)
	assert.Equal(t, 65535, got.Port)
}
