package httptracker

import (
	"net/http"
	"strconv"
)

// StatusError is returned from HTTP tracker announces when the response code is not 200 OK.
type StatusError struct {
	Code   int
	Header http.Header
	Body   string
}

func (e *StatusError) Error() string {
	return "http status: " + strconv.Itoa(e.Code)
}
