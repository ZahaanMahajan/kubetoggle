package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

func New(level string, output io.Writer) (*slog.Logger, error) {
	var logLevel slog.Level

	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		return nil, fmt.Errorf(
			"invalid log level %q: expected debug, info, warn, or error",
			level,
		)
	}

	handler := slog.NewTextHandler(output, &slog.HandlerOptions{
		Level: logLevel,
	})

	return slog.New(handler), nil
}
