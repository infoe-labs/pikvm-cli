package cli

import (
	"context"
	"errors"
	"io/fs"
)

// ErrorMessage prevents local filenames from being printed by the executable.
// API/transport errors also avoid echoing server payloads or device hostnames.
func ErrorMessage(err error) string {
	if errors.Is(err, context.Canceled) {
		return "operation canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "operation timed out"
	}
	var pathError *fs.PathError
	if errors.As(err, &pathError) {
		return "file operation failed; check file access and available storage"
	}
	return err.Error()
}
