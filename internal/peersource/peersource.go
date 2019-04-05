package peersource

type Source int

const (
	Tracker Source = iota
	DHT
	PEX
	Manual
	Incoming
	Magnet
)

func (s Source) String() string {
	switch s {
	case Tracker:
		return "tracker"
	case DHT:
		return "dht"
	case PEX:
		return "pex"
	case Manual:
		return "manual"
	case Incoming:
		return "incoming"
	case Magnet:
		return "magnet"
	default:
		panic("unhandled source")
	}
}
