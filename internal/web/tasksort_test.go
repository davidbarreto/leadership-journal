package web

import (
	"net/url"
	"slices"
	"testing"
	"time"

	"leadership-journal/internal/domain"
)

func TestTaskSortFrom(t *testing.T) {
	cases := []struct {
		query string
		want  taskSort
	}{
		{"", defaultTaskSort},
		{"sort=bogus&dir=asc", defaultTaskSort},
		{"sort=ticket", taskSort{Key: sortTicket}},
		{"sort=ticket&dir=desc", taskSort{Key: sortTicket, Desc: true}},
		{"sort=status&dir=sideways", taskSort{Key: sortStatus}},
	}
	for _, c := range cases {
		q, _ := url.ParseQuery(c.query)
		if got := taskSortFrom(q); got != c.want {
			t.Errorf("taskSortFrom(%q) = %+v, want %+v", c.query, got, c.want)
		}
	}
}

func TestTaskSortHeaders_SortedColumnFlips(t *testing.T) {
	heads := taskSortHeaders("/people/1?tab=work", taskSort{Key: sortRelease, Desc: true})
	if len(heads) != len(taskColumns) {
		t.Fatalf("got %d headers, want %d", len(heads), len(taskColumns))
	}
	for _, h := range heads {
		switch h.Label {
		case "Release":
			if h.URL != "/people/1?tab=work&sort=release&dir=asc" || h.AriaSort != "descending" {
				t.Errorf("release header = %+v", h)
			}
		case "Ticket":
			if h.URL != "/people/1?tab=work&sort=ticket&dir=asc" || h.AriaSort != "" {
				t.Errorf("ticket header = %+v", h)
			}
		}
	}
	asc := taskSortHeaders("/x?a=b", taskSort{Key: sortTicket})
	if asc[1].URL != "/x?a=b&sort=ticket&dir=desc" || asc[1].AriaSort != "ascending" {
		t.Errorf("ascending ticket header = %+v", asc[1])
	}
}

func TestSortWorkRows(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2024, 1, d, 0, 0, 0, 0, time.UTC) }
	releases := map[int64]domain.Release{1: {ID: 1, StartsOn: day(1)}, 2: {ID: 2, StartsOn: day(20)}}
	row := func(title, ticket string, release int64, s domain.Stretch, p domain.PickedBy, st domain.WorkStatus) workRow {
		return workRow{Item: domain.WorkItem{Title: title, ExternalRef: ticket, ReleaseID: release, Stretch: s, PickedBy: p, Status: st}}
	}
	base := []workRow{
		row("b task", "PAY-10", 1, domain.StretchAbove, domain.PickedByLead, domain.WorkDone),
		row("A task", "", 2, domain.StretchBelow, domain.PickedBySelf, domain.WorkMoved),
		row("c task", "pay-9", 2, domain.StretchAt, domain.PickedByLead, domain.WorkPlanned),
		row("d task", "OPS-2", 1, domain.StretchAt, domain.PickedBySelf, domain.WorkInProgress),
	}
	cases := []struct {
		by   taskSort
		want []string
	}{
		{taskSort{Key: sortTask}, []string{"A task", "b task", "c task", "d task"}},
		{taskSort{Key: sortTicket}, []string{"d task", "c task", "b task", "A task"}},
		{taskSort{Key: sortTicket, Desc: true}, []string{"b task", "c task", "d task", "A task"}},
		// Ties keep the incoming order in both directions.
		{taskSort{Key: sortRelease}, []string{"b task", "d task", "A task", "c task"}},
		{defaultTaskSort, []string{"A task", "c task", "b task", "d task"}},
		{taskSort{Key: sortStretch}, []string{"A task", "c task", "d task", "b task"}},
		{taskSort{Key: sortPicked}, []string{"b task", "c task", "A task", "d task"}},
		{taskSort{Key: sortStatus}, []string{"c task", "d task", "b task", "A task"}},
	}
	for _, c := range cases {
		rows := slices.Clone(base)
		sortWorkRows(rows, c.by, releases)
		var got []string
		for _, r := range rows {
			got = append(got, r.Item.Title)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("sort %+v = %v, want %v", c.by, got, c.want)
		}
	}
}

func TestNaturalCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"pay-9", "pay-10", -1},
		{"pay-10", "pay-9", 1},
		{"pay-010", "pay-10", 0},
		{"ops-2", "pay-1", -1},
		{"pay", "pay-1", -1},
		{"pay-1", "pay-1", 0},
		{"", "a", -1},
	}
	for _, c := range cases {
		if got := naturalCompare(c.a, c.b); got != c.want {
			t.Errorf("naturalCompare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
