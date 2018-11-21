package boltdbresumer

type Spec struct {
	InfoHash []byte
	Dest     string
	Port     int
	Name     string
	Trackers []string
	Info     []byte
	Bitfield []byte
}
