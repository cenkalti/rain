package peer

type Source int

const (
	SourceTracker Source = iota
	SourceDHT
	SourcePEX
	SourceManual
	SourceIncoming
)
