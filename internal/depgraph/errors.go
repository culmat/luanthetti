package depgraph

// NotFoundError is returned when a package or game spec matches nothing. Its message is the
// same text ldg prints, so CLI output matches.
type NotFoundError struct{ Msg string }

func (e *NotFoundError) Error() string { return e.Msg }
