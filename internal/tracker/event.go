package tracker

// Event type that is sent in an announce request.
type Event int32

// Tracker Announce Events. Numbers corresponds to constants in UDP tracker protocol.
const (
	EventNone Event = iota
	EventCompleted
	EventStarted
	EventStopped
)

var eventNames = [...]string{
	"empty",
	"completed",
	"started",
	"stopped",
}

// String returns the name of event as represented in HTTP tracker protocol.
func (e Event) String() string {
	return eventNames[e]
}
