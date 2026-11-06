package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewAcceptsValidLogLevels(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error"}

	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			logger, err := New(level, &bytes.Buffer{})
			if err != nil {
				t.Fatalf("New(%q) error = %v", level, err)
			}

			if logger == nil {
				t.Fatal("New() returned nil logger")
			}
		})
	}
}

func TestNewRejectsInvalidLogLevel(t *testing.T) {
	invalidLevels := []string{"trace", "fatal", "", "invalid"}

	for _, level := range invalidLevels {
		t.Run(level, func(t *testing.T) {
			logger, err := New(level, &bytes.Buffer{})

			if err == nil {
				t.Fatal("New() error = nil, want an error")
			}

			if logger != nil {
				t.Fatal("New() returned a logger for an invalid level")
			}

			if !strings.Contains(err.Error(), "invalid log level") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestNewFiltersMessagesByLevel(t *testing.T) {
	tests := []struct {
		name       string
		level      string
		log        func(*slog.Logger)
		expected   string
		unexpected string
	}{
		{
			name:  "debug level includes debug messages",
			level: "debug",
			log: func(logger *slog.Logger) {
				logger.Debug("debug message")
			},
			expected: "debug message",
		},
		{
			name:  "info level excludes debug messages",
			level: "info",
			log: func(logger *slog.Logger) {
				logger.Debug("debug message")
			},
			unexpected: "debug message",
		},
		{
			name:  "warn level excludes info messages",
			level: "warn",
			log: func(logger *slog.Logger) {
				logger.Info("info message")
			},
			unexpected: "info message",
		},
		{
			name:  "error level excludes warn messages",
			level: "error",
			log: func(logger *slog.Logger) {
				logger.Warn("warn message")
			},
			unexpected: "warn message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer

			logger, err := New(tt.level, &output)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			tt.log(logger)

			got := output.String()

			if tt.expected != "" && !strings.Contains(got, tt.expected) {
				t.Errorf("output %q does not contain %q", got, tt.expected)
			}

			if tt.unexpected != "" && strings.Contains(got, tt.unexpected) {
				t.Errorf("output %q unexpectedly contains %q", got, tt.unexpected)
			}
		})
	}
}

func TestNewAcceptsCaseInsensitiveLevels(t *testing.T) {
	for _, level := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		if _, err := New(level, &bytes.Buffer{}); err != nil {
			t.Errorf("New(%q) error = %v", level, err)
		}
	}
}
