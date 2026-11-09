package bmstuvpn

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ringLog хранит последние строки журнала minivpn и пишет их в logcat через stderr.
type ringLog struct {
	mu    sync.Mutex
	lines []string
	max   int
	auth  bool
}

func newRingLog(max int) *ringLog {
	return &ringLog{max: max}
}

func (l *ringLog) add(level, msg string) {
	if strings.Contains(msg, "AUTH_FAILED") {
		l.mu.Lock()
		l.auth = true
		l.mu.Unlock()
	}
	line := time.Now().Format("15:04:05") + " " + level + " " + msg
	l.mu.Lock()
	l.lines = append(l.lines, line)
	if len(l.lines) > l.max {
		l.lines = l.lines[len(l.lines)-l.max:]
	}
	l.mu.Unlock()
}

// authHint дополняет ошибку рукопожатия, если сервер отклонил логин или пароль.
func (l *ringLog) authHint() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.auth {
		return " (AUTH_FAILED: неверный логин или пароль)"
	}
	return ""
}

func (l *ringLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// Отладочные сообщения minivpn содержат ключи шифрования, поэтому не сохраняются.
func (l *ringLog) Debug(string)                  {}
func (l *ringLog) Debugf(string, ...any)         {}
func (l *ringLog) Info(msg string)               { l.add("I", msg) }
func (l *ringLog) Infof(format string, v ...any) { l.add("I", fmt.Sprintf(format, v...)) }
func (l *ringLog) Warn(msg string)               { l.add("W", msg) }
func (l *ringLog) Warnf(format string, v ...any) { l.add("W", fmt.Sprintf(format, v...)) }
