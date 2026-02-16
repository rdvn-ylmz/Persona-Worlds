package observability

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type Fields map[string]any

type Logger struct {
	service string
	out     io.Writer
	mu      sync.Mutex
}

func NewLogger(service string) *Logger {
	return &Logger{
		service: strings.TrimSpace(service),
		out:     os.Stdout,
	}
}

func (l *Logger) Info(msg string, fields Fields) {
	l.log("info", msg, fields)
}

func (l *Logger) Warn(msg string, fields Fields) {
	l.log("warn", msg, fields)
}

func (l *Logger) Error(msg string, fields Fields) {
	l.log("error", msg, fields)
}

func (l *Logger) log(level, msg string, fields Fields) {
	if l == nil {
		return
	}

	entry := map[string]any{
		"ts":      time.Now().UTC().Format(time.RFC3339Nano),
		"level":   strings.TrimSpace(level),
		"msg":     strings.TrimSpace(msg),
		"service": strings.TrimSpace(l.service),
	}
	for key, value := range fields {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		if stringValue, ok := value.(string); ok && strings.TrimSpace(stringValue) == "" {
			continue
		}
		entry[strings.TrimSpace(key)] = value
	}

	payload, err := json.Marshal(entry)
	if err != nil {
		payload = []byte(fmt.Sprintf(`{"ts":"%s","level":"error","msg":"logger_marshal_failed","service":"%s"}`,
			time.Now().UTC().Format(time.RFC3339Nano),
			strings.TrimSpace(l.service),
		))
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintln(l.out, string(payload))
}
