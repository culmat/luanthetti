package cli

import "fmt"

// Exit codes, the same as luma's.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// UsageError is a wrong invocation (unknown command or flag, bad argument count). It exits with 2.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

func usagef(format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}
