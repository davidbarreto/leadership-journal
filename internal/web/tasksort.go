package web

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode"

	"leadership-journal/internal/domain"
)

// taskSortKey names a sortable column of a task table, as used in ?sort=.
type taskSortKey string

// The sortable task columns.
const (
	sortTask    taskSortKey = "task"
	sortTicket  taskSortKey = "ticket"
	sortRelease taskSortKey = "release"
	sortStretch taskSortKey = "stretch"
	sortPicked  taskSortKey = "picked"
	sortStatus  taskSortKey = "status"
)

// taskColumns are the sortable columns in display order, with their headers.
var taskColumns = []struct {
	Key   taskSortKey
	Label string
}{
	{sortTask, "Task"},
	{sortTicket, "Ticket"},
	{sortRelease, "Release"},
	{sortStretch, "Stretch"},
	{sortPicked, "Picked by"},
	{sortStatus, "Status"},
}

// taskSort is the order of a task table: a column and a direction.
type taskSort struct {
	Key  taskSortKey
	Desc bool
}

// defaultTaskSort shows the newest release first.
var defaultTaskSort = taskSort{Key: sortRelease, Desc: true}

// taskSortFrom reads ?sort= and ?dir=, falling back to the default for an
// unknown column. A known column without a direction sorts ascending.
func taskSortFrom(q url.Values) taskSort {
	key := taskSortKey(q.Get("sort"))
	for _, c := range taskColumns {
		if c.Key == key {
			return taskSort{Key: key, Desc: q.Get("dir") == "desc"}
		}
	}
	return defaultTaskSort
}

// sortHeader is one column header of a sortable table.
type sortHeader struct {
	Label string
	URL   string
	// AriaSort is "ascending" or "descending" on the sorted column, else empty.
	AriaSort string
}

// taskSortHeaders builds the headers of a task table at base (a URL that
// already has a query string). Clicking the sorted column flips its
// direction; any other column starts ascending.
func taskSortHeaders(base string, current taskSort) []sortHeader {
	headers := make([]sortHeader, 0, len(taskColumns))
	for _, c := range taskColumns {
		h := sortHeader{Label: c.Label}
		dir := "asc"
		if c.Key == current.Key {
			h.AriaSort = "ascending"
			if current.Desc {
				h.AriaSort = "descending"
			} else {
				dir = "desc"
			}
		}
		h.URL = fmt.Sprintf("%s&sort=%s&dir=%s", base, c.Key, dir)
		headers = append(headers, h)
	}
	return headers
}

// Display orders of the enum columns: by size of the stretch, and by how far
// along the work is (a moved task left the person, so it comes last).
var (
	stretchOrder = []domain.Stretch{domain.StretchBelow, domain.StretchAt, domain.StretchAbove}
	statusOrder  = []domain.WorkStatus{domain.WorkPlanned, domain.WorkInProgress, domain.WorkDone, domain.WorkDropped, domain.WorkMoved}
)

// sortWorkRows orders rows in place. Ties keep their incoming order, and tasks
// without a ticket stay last in both directions when sorting by ticket.
// Releases are ordered by start date, looked up in releases.
func sortWorkRows(rows []workRow, by taskSort, releases map[int64]domain.Release) {
	compare := func(a, b workRow) int {
		switch by.Key {
		case sortTask:
			return cmp.Compare(strings.ToLower(a.Item.Title), strings.ToLower(b.Item.Title))
		case sortTicket:
			return naturalCompare(strings.ToLower(a.Item.ExternalRef), strings.ToLower(b.Item.ExternalRef))
		case sortRelease:
			return releases[a.Item.ReleaseID].StartsOn.Compare(releases[b.Item.ReleaseID].StartsOn)
		case sortStretch:
			return cmp.Compare(slices.Index(stretchOrder, a.Item.Stretch), slices.Index(stretchOrder, b.Item.Stretch))
		case sortPicked:
			return cmp.Compare(a.Item.PickedBy, b.Item.PickedBy)
		case sortStatus:
			return cmp.Compare(slices.Index(statusOrder, a.Item.Status), slices.Index(statusOrder, b.Item.Status))
		}
		return 0
	}
	slices.SortStableFunc(rows, func(a, b workRow) int {
		if by.Key == sortTicket {
			switch aBlank, bBlank := a.Item.ExternalRef == "", b.Item.ExternalRef == ""; {
			case aBlank && !bBlank:
				return 1
			case bBlank && !aBlank:
				return -1
			}
		}
		if by.Desc {
			return compare(b, a)
		}
		return compare(a, b)
	})
}

// naturalCompare compares strings with digit runs taken as numbers, so that
// ticket "PAY-9" comes before "PAY-10".
func naturalCompare(a, b string) int {
	for a != "" && b != "" {
		ca, ra := leadingRun(a)
		cb, rb := leadingRun(b)
		if c := compareRun(ca, cb); c != 0 {
			return c
		}
		a, b = ra, rb
	}
	return cmp.Compare(len(a), len(b))
}

// leadingRun splits s after its first run of digits or of non-digits.
func leadingRun(s string) (run, rest string) {
	digit := unicode.IsDigit(rune(s[0]))
	i := strings.IndexFunc(s, func(r rune) bool { return unicode.IsDigit(r) != digit })
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i:]
}

// compareRun compares two runs, numerically when both are digits. Leading
// zeros are ignored, so a longer number is the larger one.
func compareRun(a, b string) int {
	if a == "" || b == "" || !unicode.IsDigit(rune(a[0])) || !unicode.IsDigit(rune(b[0])) {
		return cmp.Compare(a, b)
	}
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if c := cmp.Compare(len(a), len(b)); c != 0 {
		return c
	}
	return cmp.Compare(a, b)
}
