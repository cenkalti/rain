package boltdbresumer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshalUnmarshalSpec(t *testing.T) {
	s := Spec{
		Info: []byte{1, 2, 3},
		Name: "foo",
	}
	b, err := s.MarshalJSON()
	require.NoError(t, err)

	var got Spec
	require.NoError(t, got.UnmarshalJSON(b))
	assert.Equal(t, s.Info, got.Info)
	assert.Equal(t, s.Name, got.Name)
}

// The JSON form is what the RPC layer ships, so every field has to survive it.
// InfoHash, Info and Bitfield go through base64 and SeededFor through an int64,
// which is where a round-trip is most likely to lose something.
func TestMarshalUnmarshalSpecAllFields(t *testing.T) {
	want := fullSpec()

	b, err := want.MarshalJSON()
	require.NoError(t, err)

	var got Spec
	require.NoError(t, got.UnmarshalJSON(b))

	assert.Equal(t, want.InfoHash, got.InfoHash)
	assert.Equal(t, want.Port, got.Port)
	assert.Equal(t, want.Name, got.Name)
	assert.Equal(t, want.Trackers, got.Trackers)
	assert.Equal(t, want.URLList, got.URLList)
	assert.Equal(t, want.FixedPeers, got.FixedPeers)
	assert.Equal(t, want.Info, got.Info)
	assert.Equal(t, want.Bitfield, got.Bitfield)
	assert.True(t, want.AddedAt.Equal(got.AddedAt), "AddedAt: want %s got %s", want.AddedAt, got.AddedAt)
	assert.Equal(t, want.BytesDownloaded, got.BytesDownloaded)
	assert.Equal(t, want.BytesUploaded, got.BytesUploaded)
	assert.Equal(t, want.BytesWasted, got.BytesWasted)
	assert.Equal(t, want.SeededFor, got.SeededFor)
	assert.Equal(t, want.Started, got.Started)
	assert.Equal(t, want.StopAfterDownload, got.StopAfterDownload)
	assert.Equal(t, want.StopAfterMetadata, got.StopAfterMetadata)
	assert.Equal(t, want.CompleteCmdRun, got.CompleteCmdRun)
	assert.Equal(t, want.Sequential, got.Sequential)
	assert.Equal(t, want.Version, got.Version)
}

// Unlike the BoltDB encoding, JSON carries AddedAt as a time.Time, so it keeps
// sub-second precision. Worth pinning: the two encodings differ here.
func TestMarshalJSONKeepsSubSecondAddedAt(t *testing.T) {
	s := fullSpec()
	s.AddedAt = time.Date(2026, 3, 1, 12, 30, 45, 123456789, time.UTC)

	b, err := s.MarshalJSON()
	require.NoError(t, err)

	var got Spec
	require.NoError(t, got.UnmarshalJSON(b))
	assert.True(t, s.AddedAt.Equal(got.AddedAt), "want %s got %s", s.AddedAt, got.AddedAt)
}

func TestUnmarshalSpecRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"not json", `{`},
		{"wrong type", `[]`},
		{"bad base64 info hash", `{"InfoHash":"!!!not base64!!!"}`},
		{"bad base64 info", `{"Info":"!!!not base64!!!"}`},
		{"bad base64 bitfield", `{"Bitfield":"!!!not base64!!!"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var s Spec
			assert.Error(t, s.UnmarshalJSON([]byte(c.in)))
		})
	}
}

func TestUnmarshalSpecEmptyObject(t *testing.T) {
	var s Spec
	require.NoError(t, s.UnmarshalJSON([]byte(`{}`)))
	assert.Empty(t, s.InfoHash)
	assert.Empty(t, s.Info)
	assert.Empty(t, s.Bitfield)
	assert.Zero(t, s.SeededFor)
	assert.Zero(t, s.Version)
}
