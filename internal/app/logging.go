package app

import (
	"log"
	"strings"
)

// Child loggers share the base logger's serialization, even if its Writer (for
// example a test buffer) is not safe for concurrent writes by separate loggers.
func componentLogger(base *log.Logger, name string) *log.Logger {
	return prefixedLogger(base, "["+name+"] ")
}

func prefixedLogger(base *log.Logger, prefix string) *log.Logger {
	return log.New(prefixedLogWriter{base: base, prefix: prefix}, "", 0)
}

type prefixedLogWriter struct {
	base   *log.Logger
	prefix string
}

func (w prefixedLogWriter) Write(data []byte) (int, error) {
	if err := w.base.Output(3, w.prefix+strings.TrimSuffix(string(data), "\n")); err != nil {
		return 0, err
	}
	return len(data), nil
}
