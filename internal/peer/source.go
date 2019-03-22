package peer

type Source int

const (
	SourceTracker Source = iota
	SourceDHT
	SourcePEX
	SourceManual
	SourceIncoming
	SourceWebseed
)

func (s Source) String() string {
	switch s {
	case SourceTracker:
		return "tracker"
	case SourceDHT:
		return "dht"
	case SourcePEX:
		return "pex"
	case SourceManual:
		return "manual"
	case SourceIncoming:
		return "incoming"
	case SourceWebseed:
		return "webseed"
	default:
		panic("unhandled source")
	}
}
