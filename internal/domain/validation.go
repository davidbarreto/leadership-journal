package domain

import "errors"

// ValidationError is a problem with what the user entered or a rule of the
// journal they ran into, as opposed to a fault of the system. The web layer
// shows its message to the user instead of reporting an internal error.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(msg string) error { return &ValidationError{msg: msg} }

// IsValidation reports whether err, or any error it wraps or joins, is a
// ValidationError.
func IsValidation(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}
