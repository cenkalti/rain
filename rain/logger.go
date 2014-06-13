package rain

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/cenkalti/log"
)

type logger log.Logger

var LogLevel = log.INFO

func newLogger(name string) logger {
	handler := log.NewWriterHandler(os.Stderr)
	handler.SetFormatter(logFormatter{})
	handler.Colorize = true

	logger := log.NewLogger(name)
	logger.SetHandler(handler)

	handler.SetLevel(LogLevel)
	logger.SetLevel(LogLevel)
	return logger
}

type logFormatter struct{}

// Format outputs a message like "2014-02-28 18:15:57 [example] INFO     somethinfig happened"
func (f logFormatter) Format(rec *log.Record) string {
	return fmt.Sprintf("%s %-8s [%s] %-8s %s", fmt.Sprint(rec.Time)[:19], log.LevelNames[rec.Level], rec.LoggerName, filepath.Base(rec.Filename)+":"+strconv.Itoa(rec.Line), rec.Message)
}

func init() {
	log.DefaultHandler.SetFormatter(logFormatter{})
}
