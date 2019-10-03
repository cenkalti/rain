package httptracker

import (
	"strconv"
)

// StatusError is returned from HTTP tracker announces when the response code is not 200 OK.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string {
	return "http status: " + strconv.Itoa(e.Code)
}
