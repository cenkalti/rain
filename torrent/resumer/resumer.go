// Package resumer contains an interface that is used by torrent package for resuming an existing download.
package resumer

// Resumer provides operations to save and load resume info for a Torrent.
type Resumer interface {
	Write(spec *Spec) error
	WriteInfo([]byte) error
	WriteBitfield([]byte) error
}

type Spec struct {
	InfoHash    []byte
	Port        int
	Name        string
	Trackers    []string
	StorageType string
	StorageArgs map[string]interface{}
	Info        []byte
	Bitfield    []byte
}
