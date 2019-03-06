package btconn

var (
	errInvalidInfoHash = &Error{"invalid info hash"}
	errOwnConnection   = &Error{"dropped own connection"}
	errNotEncrypted    = &Error{"connection is not encrypted"}
)

type Error struct {
	message string
}

func (e *Error) Error() string {
	return e.message
}
