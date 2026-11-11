package app

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Журнал приложения: файл app.log в папке настроек (аналог logcat) и последние строки в памяти.

var (
	logMu    sync.Mutex
	logLines []string
	logger   = log.New(os.Stderr, "", log.LstdFlags)
)

// InitLog направляет журнал в dir/app.log (прежний журнал переименовывается в app.log.1).
func InitLog(dir string) {
	path := filepath.Join(dir, "app.log")
	_ = os.Rename(path, path+".1")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	var w io.Writer = f
	if os.Stderr != nil {
		if _, err := os.Stderr.Stat(); err == nil {
			w = io.MultiWriter(f, os.Stderr)
		}
	}
	logger.SetOutput(w)
}

func logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	logger.Print(msg)
	logMu.Lock()
	logLines = append(logLines, time.Now().Format("15:04:05")+" "+msg)
	if len(logLines) > 300 {
		logLines = logLines[len(logLines)-300:]
	}
	logMu.Unlock()
}

// Logf — запись в журнал приложения.
func Logf(format string, args ...any) { logf(format, args...) }

// AppLog — последние строки журнала.
func AppLog() string {
	logMu.Lock()
	defer logMu.Unlock()
	return strings.Join(logLines, "\n")
}
