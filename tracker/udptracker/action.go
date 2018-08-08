package udptracker

type action int32

// UDP tracker Actions
const (
	actionConnect action = iota
	actionAnnounce
	actionScrape
	actionError
)
