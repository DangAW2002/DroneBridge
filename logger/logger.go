package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// Level represents logging level
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

var levelNames = map[Level]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
}

var levelFromString = map[string]Level{
	"debug": DEBUG,
	"info":  INFO,
	"warn":  WARN,
	"error": ERROR,
}

// Logger is a leveled logger
type Logger struct {
	mu          sync.RWMutex
	level       Level
	logger      *log.Logger
	useUnixTime bool
}

var defaultLogger = &Logger{
	level:       INFO,
	logger:      log.New(os.Stdout, "", log.LstdFlags),
	useUnixTime: false,
}

// SetLevel sets the global log level
func SetLevel(level Level) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.level = level
}

// SetLevelFromString sets log level from string (debug, info, warn, error)
func SetLevelFromString(levelStr string) {
	if level, ok := levelFromString[strings.ToLower(levelStr)]; ok {
		SetLevel(level)
		defaultLogger.logger.Printf("[LOGGER] Log level set to %s", levelNames[level])
	}
}

// SetTimestampFormat sets timestamp format ("time" or "unix")
func SetTimestampFormat(format string) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()

	if strings.ToLower(format) == "unix" {
		defaultLogger.useUnixTime = true
		defaultLogger.logger.SetFlags(0) // Remove default timestamp
		defaultLogger.logger.Printf("[%d] [LOGGER] Timestamp format set to Unix", time.Now().Unix())
	} else {
		defaultLogger.useUnixTime = false
		defaultLogger.logger.SetFlags(log.LstdFlags)
		defaultLogger.logger.Printf("[LOGGER] Timestamp format set to Time")
	}
}

// GetLevel returns current log level
func GetLevel() Level {
	defaultLogger.mu.RLock()
	defer defaultLogger.mu.RUnlock()
	return defaultLogger.level
}

// GetLevelString returns current log level as string
func GetLevelString() string {
	return levelNames[GetLevel()]
}

func shouldLog(level Level) bool {
	defaultLogger.mu.RLock()
	defer defaultLogger.mu.RUnlock()
	return level >= defaultLogger.level
}

// formatMessage adds timestamp prefix if using Unix time
func formatMessage(prefix, format string, v ...interface{}) string {
	defaultLogger.mu.RLock()
	useUnix := defaultLogger.useUnixTime
	defaultLogger.mu.RUnlock()

	if useUnix {
		return fmt.Sprintf("[%d] %s%s", time.Now().Unix(), prefix, fmt.Sprintf(format, v...))
	}
	return fmt.Sprintf("%s%s", prefix, fmt.Sprintf(format, v...))
}

// Debug logs at DEBUG level
func Debug(format string, v ...interface{}) {
	if shouldLog(DEBUG) {
		defaultLogger.logger.Print(formatMessage("[DEBUG] ", format, v...))
	}
}

// Info logs at INFO level
func Info(format string, v ...interface{}) {
	if shouldLog(INFO) {
		defaultLogger.logger.Print(formatMessage("[INFO] ", format, v...))
	}
}

// Warn logs at WARN level
func Warn(format string, v ...interface{}) {
	if shouldLog(WARN) {
		defaultLogger.logger.Print(formatMessage("[WARN] ", format, v...))
	}
}

// Error logs at ERROR level
func Error(format string, v ...interface{}) {
	if shouldLog(ERROR) {
		defaultLogger.logger.Print(formatMessage("[ERROR] ", format, v...))
	}
}

// Debugf is alias for Debug
func Debugf(format string, v ...interface{}) {
	Debug(format, v...)
}

// Infof is alias for Info
func Infof(format string, v ...interface{}) {
	Info(format, v...)
}

// Warnf is alias for Warn
func Warnf(format string, v ...interface{}) {
	Warn(format, v...)
}

// Errorf is alias for Error
func Errorf(format string, v ...interface{}) {
	Error(format, v...)
}

// Fatal logs at ERROR level and exits
func Fatal(format string, v ...interface{}) {
	defaultLogger.logger.Print(formatMessage("[FATAL] ", format, v...))
	os.Exit(1)
}

// Fatalf is alias for Fatal
func Fatalf(format string, v ...interface{}) {
	Fatal(format, v...)
}

// Printf always logs (for backward compatibility)
func Printf(format string, v ...interface{}) {
	defaultLogger.logger.Printf(format, v...)
}

// Println always logs (for backward compatibility)
func Println(v ...interface{}) {
	defaultLogger.logger.Println(v...)
}

// String returns formatted string without logging
func String(format string, v ...interface{}) string {
	return fmt.Sprintf(format, v...)
}
