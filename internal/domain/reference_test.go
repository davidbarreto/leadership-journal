package domain

import "testing"

func TestLevel_Validate(t *testing.T) {
	tests := []struct {
		name    string
		l       Level
		wantErr []error
	}{
		{name: "valid", l: Level{Track: TrackQA, Code: "QA1", Title: "Junior", Rank: 10}},
		{name: "track required", l: Level{Code: "QA1", Title: "Junior", Rank: 10}, wantErr: []error{ErrTrackRequired}},
		{name: "code required", l: Level{Track: TrackQA, Title: "Junior", Rank: 10}, wantErr: []error{ErrCodeRequired}},
		{name: "title required", l: Level{Track: TrackQA, Code: "QA1", Rank: 10}, wantErr: []error{ErrTitleRequired}},
		{name: "rank must be positive", l: Level{Track: TrackQA, Code: "QA1", Title: "Junior"}, wantErr: []error{ErrInvalidRank}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertErrIs(t, tc.l.Validate(), tc.wantErr...)
		})
	}
}

func TestCodeFromLabel(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Skip-level 1:1", "skip_level_1_1"},
		{"  Retro  ", "retro"},
		{"Café chat", "caf_chat"},
		{"already_snake", "already_snake"},
		{"---", ""},
		{"", ""},
	}
	for _, tc := range tests {
		if got := CodeFromLabel(tc.in); got != tc.want {
			t.Errorf("CodeFromLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
