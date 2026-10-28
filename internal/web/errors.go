package web

import (
	"errors"
	"strings"

	"leadership-journal/internal/domain"
	"leadership-journal/internal/store"
)

// isUserError reports whether err is something the user can act on (a mistake
// in the input or a journal rule) rather than a fault of the system.
func isUserError(err error) bool {
	var bi *badInput
	switch {
	case errors.As(err, &bi), domain.IsValidation(err):
		return true
	}
	for _, sentinel := range userStoreErrors {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

var userStoreErrors = []error{
	store.ErrDuplicate, store.ErrNotDraft, store.ErrNotFinalized,
	store.ErrReasonRequired, store.ErrCannotMove, store.ErrFinalizeWithMeeting,
}

// userMessage is the text shown to the user. Store errors map to fixed
// sentences because their own text can carry SQL details.
func userMessage(err error) string {
	switch {
	case errors.Is(err, store.ErrDuplicate):
		return "That already exists."
	case errors.Is(err, store.ErrNotDraft):
		return "That record is finalized, so it can no longer be changed."
	case errors.Is(err, store.ErrNotFinalized):
		return "Only finalized records can be voided; delete a draft instead."
	case errors.Is(err, store.ErrReasonRequired):
		return "Please give a reason."
	}
	// Joined validation errors come one per line; show them on one line.
	msg := strings.Join(strings.Split(err.Error(), "\n"), "; ")
	if msg == "" {
		return "Invalid input."
	}
	return strings.ToUpper(msg[:1]) + msg[1:] + "."
}
