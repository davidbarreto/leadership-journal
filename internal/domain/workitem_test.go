package domain

import (
	"testing"
	"time"
)

func validWorkItem() WorkItem {
	return WorkItem{
		PersonID:         1,
		ReleaseID:        1,
		Title:            "Migrate billing job",
		PickedBy:         PickedByLead,
		Stretch:          StretchAbove,
		StretchRationale: "first time touching billing",
		Status:           WorkPlanned,
		AssignedAt:       day(2024, 3, 1),
	}
}

func TestWorkItem_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*WorkItem)
		wantErr []error
	}{
		{name: "valid", mutate: func(*WorkItem) {}},
		{name: "blank title", mutate: func(w *WorkItem) { w.Title = "  " }, wantErr: []error{ErrTitleRequired}},
		{name: "missing person", mutate: func(w *WorkItem) { w.PersonID = 0 }, wantErr: []error{ErrPersonRequired}},
		{name: "missing release", mutate: func(w *WorkItem) { w.ReleaseID = 0 }, wantErr: []error{ErrReleaseRequired}},
		{name: "invalid picked by", mutate: func(w *WorkItem) { w.PickedBy = "boss" }, wantErr: []error{ErrInvalidPickedBy}},
		{name: "invalid stretch", mutate: func(w *WorkItem) { w.Stretch = "huge" }, wantErr: []error{ErrInvalidStretch}},
		{name: "invalid status", mutate: func(w *WorkItem) { w.Status = "paused" }, wantErr: []error{ErrInvalidWorkStatus}},
		{name: "rationale required", mutate: func(w *WorkItem) { w.StretchRationale = "" }, wantErr: []error{ErrRationaleRequired}},
		{name: "assigned date required", mutate: func(w *WorkItem) { w.AssignedAt = time.Time{} }, wantErr: []error{ErrAssignedAtRequired}},
		{
			name:    "all problems reported together",
			mutate:  func(w *WorkItem) { w.Title = ""; w.Stretch = "x" },
			wantErr: []error{ErrTitleRequired, ErrInvalidStretch},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := validWorkItem()
			tc.mutate(&w)
			assertErrIs(t, w.Validate(), tc.wantErr...)
		})
	}
}

func item(stretch Stretch, picked PickedBy, status WorkStatus, lc Lifecycle) WorkItem {
	w := validWorkItem()
	w.Stretch, w.PickedBy, w.Status, w.Lifecycle = stretch, picked, status, lc
	return w
}

func TestReleaseCompliant(t *testing.T) {
	fin := finalizedLifecycle()
	voided := fin
	voided.VoidedAt = ptr(day(2024, 2, 1))

	tests := []struct {
		name  string
		items []WorkItem
		want  bool
	}{
		{name: "no items", want: false},
		{name: "one above item", items: []WorkItem{item(StretchAbove, PickedBySelf, WorkInProgress, fin)}, want: true},
		{name: "only at and below", items: []WorkItem{item(StretchAt, PickedBySelf, WorkDone, fin), item(StretchBelow, PickedByLead, WorkDone, fin)}, want: false},
		{name: "above but dropped", items: []WorkItem{item(StretchAbove, PickedBySelf, WorkDropped, fin)}, want: false},
		{name: "above but moved away", items: []WorkItem{item(StretchAbove, PickedBySelf, WorkMoved, fin)}, want: false},
		{name: "above but voided", items: []WorkItem{item(StretchAbove, PickedBySelf, WorkDone, voided)}, want: false},
		{name: "above but still a draft", items: []WorkItem{item(StretchAbove, PickedBySelf, WorkDone, Lifecycle{})}, want: false},
		{name: "one valid among invalid", items: []WorkItem{item(StretchAbove, PickedBySelf, WorkDropped, fin), item(StretchAbove, PickedByLead, WorkDone, fin)}, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReleaseCompliant(tc.items); got != tc.want {
				t.Errorf("ReleaseCompliant() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestComputeWorkMetrics(t *testing.T) {
	fin := finalizedLifecycle()
	withInvestigation := item(StretchAt, PickedBySelf, WorkDone, fin)
	withInvestigation.InvestigationDone = true

	items := []WorkItem{
		item(StretchAbove, PickedBySelf, WorkDone, fin),
		item(StretchAbove, PickedBySelf, WorkInProgress, fin),
		item(StretchAbove, PickedByLead, WorkDone, fin),
		withInvestigation,
		item(StretchAbove, PickedByLead, WorkDropped, fin),      // ignored
		item(StretchAbove, PickedByLead, WorkDone, Lifecycle{}), // draft, ignored
	}

	m := ComputeWorkMetrics(items)
	if m.Total != 4 || m.Above != 3 || m.AboveSelf != 2 || m.AboveLead != 1 || m.InvestigationDone != 1 {
		t.Fatalf("metrics = %+v", m)
	}
	if got := m.PctAboveSelf(); got < 66.6 || got > 66.7 {
		t.Errorf("PctAboveSelf() = %v, want ~66.67", got)
	}
	if got := m.PctAboveLead(); got < 33.3 || got > 33.4 {
		t.Errorf("PctAboveLead() = %v, want ~33.33", got)
	}
	if got := m.PctInvestigated(); got != 25 {
		t.Errorf("PctInvestigated() = %v, want 25", got)
	}
}

func TestWorkMetrics_NoDivisionByZero(t *testing.T) {
	m := ComputeWorkMetrics(nil)
	if m.PctAboveSelf() != 0 || m.PctAboveLead() != 0 || m.PctInvestigated() != 0 {
		t.Errorf("empty metrics should be zero, got %+v", m)
	}
}
