package domain

import (
	"errors"
	"testing"
)

func TestLevel_IsAbove(t *testing.T) {
	e3 := Level{Track: TrackEngineer, Code: "E3", Rank: 30}
	e6 := Level{Track: TrackEngineer, Code: "E6", Rank: 60}
	em1 := Level{Track: TrackManager, Code: "EM1", Rank: 10}

	tests := []struct {
		name    string
		l, o    Level
		want    bool
		wantErr error
	}{
		{name: "higher rank is above", l: e6, o: e3, want: true},
		{name: "lower rank is not above", l: e3, o: e6, want: false},
		{name: "same level is not above", l: e3, o: e3, want: false},
		{name: "different tracks cannot be compared", l: e6, o: em1, wantErr: ErrDifferentTrack},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.l.IsAbove(tc.o)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("IsAbove() = %v, want %v", got, tc.want)
			}
		})
	}
}
