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

// Error implements error interface.
func (e *InputError) Error() string {
	return "input error: " + e.err.Error()
}

// Unwrap returns the underlying error.
func (e *InputError) Unwrap() error {
	return e.err
}

// AnnounceError is the error returned from announce response to a tracker.
type AnnounceError struct {
	err *announcer.AnnounceError
}

// Contains the humanized version of error.
func (e *AnnounceError) Error() string {
	return e.err.Message
}

// Unwrap returns the underlying error object.
func (e *AnnounceError) Unwrap() error {
	return e.err.Err
}

// Unknown returns true if the error is unexpected.
// Expected errors are tracker errors, network errors and DNS errors.
func (e *AnnounceError) Unknown() bool {
	return e.err.Unknown
}
