package resume

// DB provides operations to save and load resume info for a Torrent.
type DB interface {
	Read() (*Spec, error)
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
