// Package logger provides a minimal wrapper around the standard library
// logger so call sites read "logger.Infof(...)" instead of importing
// "log" directly everywhere — kept intentionally thin (no external
// logging framework dependency).
package logger

import (
	"log"
	"os"
)

var std = log.New(os.Stderr, "whisper-live: ", log.LstdFlags)

func Infof(format string, args ...any)  { std.Printf("INFO  "+format, args...) }
func Warnf(format string, args ...any)  { std.Printf("WARN  "+format, args...) }
func Errorf(format string, args ...any) { std.Printf("ERROR "+format, args...) }
