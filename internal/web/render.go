package web

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// goldmark omits raw HTML from the input by default; bluemonday then sanitizes
// whatever HTML the Markdown itself produced. Notes about people are text the
// owner typed, but they can be pasted from anywhere, so both layers stay.
var (
	markdown = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)
	sanitizer = bluemonday.UGCPolicy()
)

// renderMarkdown turns Markdown into safe HTML.
func renderMarkdown(src string) template.HTML {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(src), &buf); err != nil {
		return template.HTML(stdhtml.EscapeString(src))
	}
	return template.HTML(sanitizer.SanitizeBytes(buf.Bytes()))
}

// highlightSnippet shows a search snippet with its matches in <mark>. The text
// is escaped first and only then are the control-character markers from the
// store replaced, so the text itself can never inject markup.
func highlightSnippet(snippet string) template.HTML {
	escaped := stdhtml.EscapeString(snippet)
	escaped = strings.ReplaceAll(escaped, "\x1e", "<mark>")
	escaped = strings.ReplaceAll(escaped, "\x1f", "</mark>")
	return template.HTML(escaped)
}

// human turns a code like "improvement_point" into "Improvement point".
func human(code string) string {
	s := strings.ReplaceAll(code, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"md":        renderMarkdown,
		"highlight": highlightSnippet,
		"human":     human,
		"join":      strings.Join,
		"date": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("2 Jan 2006")
		},
		"datePtr": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2 Jan 2006")
		},
		"datetime": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("2 Jan 2006, 15:04 MST")
		},
		"inputDate": func(t time.Time) string { return t.Format("2006-01-02") },
		"inputDatePtr": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02")
		},
		"inputDateTime": func(t time.Time) string { return t.Format("2006-01-02T15:04") },
		"pct":           func(f float64) string { return fmt.Sprintf("%.0f%%", f) },
	}
}

// loadPages parses every page together with the layout and the shared
// partials (files starting with an underscore).
func loadPages() (map[string]*template.Template, error) {
	files, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	var shared, pages []string
	for _, f := range files {
		name := path.Base(f)
		if name == "layout.html" || strings.HasPrefix(name, "_") {
			shared = append(shared, f)
		} else {
			pages = append(pages, f)
		}
	}
	out := make(map[string]*template.Template, len(pages))
	for _, p := range pages {
		set := append(append([]string{}, shared...), p)
		t, err := template.New("layout.html").Funcs(funcMap()).ParseFS(assets, set...)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", p, err)
		}
		out[strings.TrimSuffix(path.Base(p), ".html")] = t
	}
	return out, nil
}

// pageData is what every template receives; Data holds the page's own model.
type pageData struct {
	Authed  bool
	Active  string
	CSRF    string
	PreAuth string
	Flash   *flash
	Now     time.Time
	Data    any
}

// render shows a page inside the layout. The page is rendered to a buffer
// first so a template error cannot leave a half-written response.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, page, active string, data any) error {
	pd := pageData{Active: active, Now: s.now(), Data: data}
	if sess, ok := sessionFromContext(r.Context()); ok {
		pd.Authed, pd.CSRF = true, sess.CSRFToken
	}
	pd.Flash = s.takeFlash(w, r)
	return s.renderPage(w, status, page, pd)
}

func (s *Server) renderPage(w http.ResponseWriter, status int, page string, pd pageData) error {
	t, ok := s.pages[page]
	if !ok {
		return fmt.Errorf("unknown page template %q", page)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", pd); err != nil {
		return fmt.Errorf("render page %q: %w", page, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

type errorPage struct {
	Code      int
	Message   string
	RequestID string
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, code int, message string) {
	data := errorPage{Code: code, Message: message, RequestID: requestID(r.Context())}
	if err := s.render(w, r, code, "error", "", data); err != nil {
		s.log.Error("could not render the error page", "error", err, "request_id", data.RequestID)
		http.Error(w, message, code)
	}
}
