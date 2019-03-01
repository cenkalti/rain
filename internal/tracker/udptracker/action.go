package udptracker

type action int32

// UDP tracker Actions
const (
	actionConnect  action = 0
	actionAnnounce action = 1
	actionError    action = 3
)
