package bot

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// Logger — потокобезопасный логгер с уровнями INFO / WARN / ERROR.
type Logger struct {
	mu     sync.Mutex
	logger *log.Logger
}

// NewLogger создаёт новый логгер, выводящий в stdout.
func NewLogger() *Logger {
	return &Logger{
		logger: log.New(os.Stdout, "", 0),
	}
}

// format добавляет префикс и время.
func (l *Logger) format(level, msg string, args ...interface{}) string {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}
	return fmt.Sprintf("[%s] [%s] %s", timestamp, level, msg)
}

// Info — сообщение уровня INFO.
func (l *Logger) Info(msg string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logger.Println(l.format("ℹ️ INFO", msg, args...))
}

// Warn — сообщение уровня WARN.
func (l *Logger) Warn(msg string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logger.Println(l.format("⚠️ WARN", msg, args...))
}

// Error — сообщение уровня ERROR.
func (l *Logger) Error(msg string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logger.Println(l.format("❌ ERROR", msg, args...))
}

// Printf — совместимость со стандартным log.Printf (если требуется).
func (l *Logger) Printf(format string, args ...interface{}) {
	l.Info(format, args...)
}
