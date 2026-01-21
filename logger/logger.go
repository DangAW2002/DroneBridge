package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
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
	mu       sync.RWMutex
	level    Level
	logger   *log.Logger
}

var defaultLogger = &Logger{
	level:  INFO,
	logger: log.New(os.Stdout, "", log.LstdFlags),
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

// Debug logs at DEBUG level
func Debug(format string, v ...interface{}) {
	if shouldLog(DEBUG) {
		defaultLogger.logger.Printf("[DEBUG] "+format, v...)
	}
}

// Info logs at INFO level
func Info(format string, v ...interface{}) {
	if shouldLog(INFO) {
		defaultLogger.logger.Printf("[INFO] "+format, v...)
	}
}

// Warn logs at WARN level
func Warn(format string, v ...interface{}) {
	if shouldLog(WARN) {
		defaultLogger.logger.Printf("[WARN] "+format, v...)
	}
}

// Error logs at ERROR level
func Error(format string, v ...interface{}) {
	if shouldLog(ERROR) {
		defaultLogger.logger.Printf("[ERROR] "+format, v...)
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
	defaultLogger.logger.Printf("[FATAL] "+format, v...)
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
