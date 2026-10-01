package logger

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
)

// LogPath returns the log file location: /tmp/k9s-deck.log, or the user's
// temp directory on Windows where /tmp doesn't exist
func LogPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.TempDir(), "k9s-deck.log")
	}
	return "/tmp/k9s-deck.log"
}

// Init initializes the structured logger to write to a file
// This avoids interfering with the TUI output
func Init() error {
	logFile, err := os.OpenFile(LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	handler := slog.NewJSONHandler(logFile, &slog.HandlerOptions{
		Level: getLogLevel(),
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)

	return nil
}

// getLogLevel returns the log level from environment variable
// Defaults to INFO if not set
func getLogLevel() slog.Level {
	switch os.Getenv("K9S_DECK_LOG_LEVEL") {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
