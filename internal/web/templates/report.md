# Report: {{.Person.Name}}

{{.Person.RoleTitle}}{{if .TeamName}} · {{.TeamName}}{{end}}{{if .Level}} · Level {{.Level}}{{end}}

Prepared on {{date .Generated}}{{if .HasPeriod}}. Period:{{if .FromInput}} from {{.FromInput}}{{end}}{{if .ToInput}} to {{.ToInput}}{{end}}{{end}}. This report contains only finalized records.
{{if .Sections.Progress}}
## Improvement journal
{{if .NoProgressStreak}}
**{{.NoProgressStreak}} progress check(s) in a row without progress.**
{{end}}{{range .Checks}}
### {{date .OccurredAt}} · {{human (print .Outcome)}}

{{.Body}}
{{else}}
No progress checks in this period.
{{end}}{{end}}{{if .Sections.Entries}}
## Notes
{{range .Entries}}
### {{.KindLabel}} · {{date .Entry.OccurredAt}}{{if .Entry.Tags}} ({{join .Entry.Tags ", "}}){{end}}

{{.Entry.Body}}
{{else}}
No notes in this period.
{{end}}{{end}}{{if .Sections.Commitments}}
## Commitments
{{range .Commitments}}
- {{.Content}} — {{human (print .Status)}}{{if .StatusNote}}: {{.StatusNote}}{{end}}{{if .DueOn}}, due {{datePtr .DueOn}}{{end}}{{if .Deliverable}} (deliverable: {{.Deliverable}}){{end}}
{{else}}
No commitments in this period.
{{end}}{{end}}{{if .Sections.Work}}
## Work

Documentation contributions: {{.DocContributions}}.
{{if .Work}}
Stretch tasks counted: {{.Metrics.Above}}; picked by them: {{pct .Metrics.PctAboveSelf}}; assigned by their lead: {{pct .Metrics.PctAboveLead}}; tasks with an investigation first: {{pct .Metrics.PctInvestigated}}.
{{range .Standings}}
- {{.Release.Name}}: {{if .Compliant}}has a stretch task{{else}}no stretch task{{end}}{{end}}
{{range .Work}}
- {{.Item.Title}} ({{.ReleaseName}}): {{human (print .Item.Stretch)}} for them, picked by {{human (print .Item.PickedBy)}}, {{human (print .Item.Status)}}. {{.Item.StretchRationale}}{{end}}
{{else}}
No tasks in this period.
{{end}}{{end}}
