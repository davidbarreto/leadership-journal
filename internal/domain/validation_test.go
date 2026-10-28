package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsValidation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"a domain sentinel", ErrNameRequired, true},
		{"a rule from the level history", ErrRankNotIncreasing, true},
		{"several problems joined", errors.Join(ErrNameRequired, ErrInvalidRelationship), true},
		{"wrapped with context", fmt.Errorf("saving person: %w", ErrNameRequired), true},
		{"a plain error", errors.New("disk on fire"), false},
		{"a plain error joined with a validation one", errors.Join(errors.New("disk on fire"), ErrNameRequired), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsValidation(tc.err); got != tc.want {
				t.Errorf("IsValidation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestValidationErrorsKeepTheirMessage(t *testing.T) {
	if got := ErrNameRequired.Error(); got != "name is required" {
		t.Errorf("message = %q", got)
	}
	if !errors.Is(fmt.Errorf("x: %w", ErrNameRequired), ErrNameRequired) {
		t.Error("errors.Is must still match the sentinel")
	}
}
