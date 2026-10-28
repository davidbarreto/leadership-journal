package domain

import (
	"errors"
	"testing"
	"time"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

var (
	e3  = Level{ID: 3, Track: TrackEngineer, Code: "E3", Rank: 30}
	e4  = Level{ID: 4, Track: TrackEngineer, Code: "E4", Rank: 40}
	e5  = Level{ID: 5, Track: TrackEngineer, Code: "E5", Rank: 50}
	e6  = Level{ID: 6, Track: TrackEngineer, Code: "E6", Rank: 60}
	em1 = Level{ID: 11, Track: TrackManager, Code: "EM1", Rank: 10}
)

func TestLevelHistory_Add(t *testing.T) {
	tests := []struct {
		name     string
		existing LevelHistory
		add      LevelAssignment
		wantErr  error
	}{
		{
			name: "first assignment",
			add:  LevelAssignment{Level: e3, EffectiveFrom: day(2023, 1, 1)},
		},
		{
			name:     "promotion to a higher rank",
			existing: LevelHistory{{Level: e3, EffectiveFrom: day(2023, 1, 1)}},
			add:      LevelAssignment{Level: e4, EffectiveFrom: day(2024, 1, 1)},
		},
		{
			name:     "same level twice",
			existing: LevelHistory{{Level: e3, EffectiveFrom: day(2023, 1, 1)}},
			add:      LevelAssignment{Level: e3, EffectiveFrom: day(2024, 1, 1)},
			wantErr:  ErrLevelAlreadyHeld,
		},
		{
			name:     "lower rank on a later date",
			existing: LevelHistory{{Level: e4, EffectiveFrom: day(2023, 1, 1)}},
			add:      LevelAssignment{Level: e3, EffectiveFrom: day(2024, 1, 1)},
			wantErr:  ErrRankNotIncreasing,
		},
		{
			name:     "backdated lower rank before an existing one",
			existing: LevelHistory{{Level: e4, EffectiveFrom: day(2024, 1, 1)}},
			add:      LevelAssignment{Level: e3, EffectiveFrom: day(2023, 1, 1)},
		},
		{
			name:     "backdated higher rank before an existing one",
			existing: LevelHistory{{Level: e4, EffectiveFrom: day(2024, 1, 1)}},
			add:      LevelAssignment{Level: e5, EffectiveFrom: day(2023, 1, 1)},
			wantErr:  ErrRankNotIncreasing,
		},
		{
			name:     "two levels on the same day",
			existing: LevelHistory{{Level: e3, EffectiveFrom: day(2024, 1, 1)}},
			add:      LevelAssignment{Level: e4, EffectiveFrom: day(2024, 1, 1)},
		},
		{
			name:     "move to another track is independent of engineer ranks",
			existing: LevelHistory{{Level: e6, EffectiveFrom: day(2023, 1, 1)}},
			add:      LevelAssignment{Level: em1, EffectiveFrom: day(2024, 1, 1)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.existing
			before := len(h)

			err := h.Add(tc.add)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Add() error = %v, want %v", err, tc.wantErr)
			}

			wantLen := before + 1
			if tc.wantErr != nil {
				wantLen = before
			}
			if len(h) != wantLen {
				t.Errorf("len(history) = %d, want %d", len(h), wantLen)
			}
		})
	}
}

func TestLevelHistory_LevelAt(t *testing.T) {
	h := LevelHistory{
		{Level: e3, EffectiveFrom: day(2023, 1, 10)},
		{Level: e4, EffectiveFrom: day(2024, 1, 10)},
	}

	tests := []struct {
		name    string
		history LevelHistory
		at      time.Time
		want    Level
		wantErr error
	}{
		{name: "empty history", history: nil, at: day(2024, 1, 1), wantErr: ErrNoLevel},
		{name: "before the first assignment", history: h, at: day(2023, 1, 9), wantErr: ErrNoLevel},
		{name: "on the effective date", history: h, at: day(2023, 1, 10), want: e3},
		{name: "between two assignments", history: h, at: day(2023, 12, 31), want: e3},
		{name: "on the next effective date", history: h, at: day(2024, 1, 10), want: e4},
		{name: "after the last assignment", history: h, at: day(2030, 1, 1), want: e4},
		{
			name:    "time of day is ignored after the effective date",
			history: h,
			at:      time.Date(2023, 1, 10, 23, 59, 0, 0, time.UTC),
			want:    e3,
		},
		{
			name:    "time of day does not pull a later date earlier",
			history: h,
			at:      time.Date(2023, 1, 9, 23, 59, 0, 0, time.UTC),
			wantErr: ErrNoLevel,
		},
		{
			name: "same day, same track: higher rank wins",
			history: LevelHistory{
				{Level: e4, EffectiveFrom: day(2024, 1, 1)},
				{Level: e3, EffectiveFrom: day(2024, 1, 1)},
			},
			at:   day(2024, 1, 1),
			want: e4,
		},
		{
			name: "track change: latest assignment wins",
			history: LevelHistory{
				{Level: e6, EffectiveFrom: day(2023, 1, 1)},
				{Level: em1, EffectiveFrom: day(2025, 1, 1)},
			},
			at:   day(2026, 1, 1),
			want: em1,
		},
		{
			name: "track change: still the old track before the move",
			history: LevelHistory{
				{Level: e6, EffectiveFrom: day(2023, 1, 1)},
				{Level: em1, EffectiveFrom: day(2025, 1, 1)},
			},
			at:   day(2024, 6, 1),
			want: e6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.history.LevelAt(tc.at)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("LevelAt() error = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("LevelAt() = %v, want %v", got, tc.want)
			}
		})
	}
}
