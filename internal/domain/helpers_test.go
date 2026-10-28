package domain

import (
	"errors"
	"testing"
)

func assertErrIs(t *testing.T, err error, wants ...error) {
	t.Helper()
	if len(wants) == 0 {
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		return
	}
	for _, want := range wants {
		if !errors.Is(err, want) {
			t.Errorf("error = %v, want it to include %v", err, want)
		}
	}
}

func finalizedLifecycle() Lifecycle {
	at := day(2024, 1, 1)
	return Lifecycle{CreatedAt: at, FinalizedAt: &at}
}

func ptr[T any](v T) *T { return &v }

func TestLifecycle(t *testing.T) {
	at := day(2024, 1, 1)
	tests := []struct {
		name                     string
		l                        Lifecycle
		draft, finalized, voided bool
	}{
		{name: "draft", l: Lifecycle{CreatedAt: at}, draft: true},
		{name: "finalized", l: Lifecycle{CreatedAt: at, FinalizedAt: &at}, finalized: true},
		{name: "voided", l: Lifecycle{CreatedAt: at, FinalizedAt: &at, VoidedAt: &at}, finalized: true, voided: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.l.IsDraft() != tc.draft || tc.l.IsFinalized() != tc.finalized || tc.l.IsVoided() != tc.voided {
				t.Errorf("draft/finalized/voided = %v/%v/%v, want %v/%v/%v",
					tc.l.IsDraft(), tc.l.IsFinalized(), tc.l.IsVoided(), tc.draft, tc.finalized, tc.voided)
			}
		})
	}
}
