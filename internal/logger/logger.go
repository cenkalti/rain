package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/cenkalti/log"
)

var handler log.Handler

func init() {
	SetHandler(log.NewFileHandler(os.Stderr))
}

// SetHandler changes the global logging handler.
func SetHandler(h log.Handler) {
	handler = h
	handler.SetFormatter(logFormatter{})
}

// SetDebug sets the logging level to DEBUG on the global handler.
func SetDebug() {
	handler.SetLevel(log.DEBUG)
}

// Disable all logging by setting a handler that discards all messages.
func Disable() {
	SetHandler(log.NewWriterHandler(io.Discard))
}

// Logger is for logging messages from inside of the program in various logging levels.
type Logger log.Logger

// New returns a new Logger with a name.
// Log messages are prefixed with this name by the default Handler.
func New(name string) Logger {
	logger := log.NewLogger(name)
	logger.SetLevel(log.DEBUG) // forward all messages to handler
	logger.SetHandler(handler)
	return logger
}

type logFormatter struct{}

// Format outputs a message like:
//
//	2014-02-28 18:15:57 [example] INFO     somethinfig happened
func (f logFormatter) Format(rec *log.Record) string {
	return fmt.Sprintf("%s %-8s [%s] %-8s %s",
		fmt.Sprint(rec.Time)[:19],
		rec.Level,
		rec.LoggerName,
		filepath.Base(rec.Filename)+":"+strconv.Itoa(rec.Line),
		rec.Message)
}
