// Package output wraps stdout/stderr writes so call sites do not have to
// import os and fmt directly.
package output

import (
	"fmt"
	"log/slog"
	"os"
)

// WriteStdout writes text verbatim to standard output.
func WriteStdout(text string) error {
	if _, err := fmt.Fprint(os.Stdout, text); err != nil {
		slog.Error("write stdout", "error", err)
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}

// WriteStderr writes text verbatim to standard error.
func WriteStderr(text string) error {
	if _, err := fmt.Fprint(os.Stderr, text); err != nil {
		slog.Error("write stderr", "error", err)
		return fmt.Errorf("write stderr: %w", err)
	}
	return nil
}
