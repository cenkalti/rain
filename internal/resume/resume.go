package resume

// ResumeInfo TODO this is for single torrent
type ResumeInfo interface {
	Read() (Spec, error)
	Write(spec Spec) error
	WriteInfo([]byte) error
	WriteBitfield([]byte) error
}

type Spec struct {
	InfoHash []byte
	Port     int
	Name     string
	Dest     string
	Trackers []string
	Info     []byte
	Bitfield []byte
}
