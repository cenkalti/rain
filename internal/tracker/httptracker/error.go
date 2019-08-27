package httptracker

import (
	"errors"
	"strconv"
)

var ErrDecode = errors.New("cannot decode response")

type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string {
	return "http status: " + strconv.Itoa(e.Code)
}
