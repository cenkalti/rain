package torrent

import (
	"github.com/cenkalti/rain/internal/announcer"
)

// InputError is returned from Session.AddTorrent and Session.AddURI methods when there is problem with the input.
type InputError struct {
	err error
}

func newInputError(err error) *InputError {
	return &InputError{
		err: err,
	}
}

func (e *InputError) Error() string {
	return "input error: " + e.err.Error()
}

func (e *InputError) Unwrap() error {
	return e.err
}

type AnnounceError struct {
	err *announcer.AnnounceError
}

func (e *AnnounceError) Error() string {
	return e.err.Message
}

func (e *AnnounceError) Unwrap() error {
	return e.err.Err
}

func (e *AnnounceError) Unknown() bool {
	return e.err.Unknown
}
