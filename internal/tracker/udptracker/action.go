package udptracker

type action int32

// UDP tracker Actions
const (
	actionConnect  action = 0
	actionAnnounce action = 1
	actionScrape   action = 2
	actionError    action = 3
)
