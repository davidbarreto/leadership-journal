package web

import (
	"errors"
	"net/http"

	"leadership-journal/internal/auth"
	"leadership-journal/internal/store"
)

type authForm struct {
	Error string
}

// renderAuthForm shows the login or setup form. The pre-login CSRF token is
// created here and checked again when the form is posted.
func (s *Server) renderAuthForm(w http.ResponseWriter, r *http.Request, status int, page, message string) error {
	token, err := s.preAuthToken(w, r)
	if err != nil {
		return err
	}
	return s.renderPage(w, status, page, pageData{PreAuth: token, Now: s.now(), Data: authForm{Error: message}})
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) error {
	has, err := s.store.HasUser(r.Context())
	if err != nil {
		return err
	}
	if !has {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return nil
	}
	if _, ok, err := s.currentSession(r); err != nil {
		return err
	} else if ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return nil
	}
	return s.renderAuthForm(w, r, http.StatusOK, "login", "")
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return errBadForm
	}
	if !checkPreAuth(r) {
		return errForbidden
	}
	hash, err := s.store.PasswordHash(r.Context())
	if errors.Is(err, store.ErrNotFound) {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return nil
	}
	if err != nil {
		return err
	}
	if !s.limiter.allowed() {
		s.log.Warn("login blocked: too many failed attempts", "request_id", requestID(r.Context()))
		return s.renderAuthForm(w, r, http.StatusTooManyRequests, "login", "Too many failed attempts. Try again in a few minutes.")
	}

	ok, err := auth.VerifyPassword(r.PostFormValue("password"), hash)
	if err != nil {
		return err
	}
	if !ok {
		s.limiter.recordFailure()
		s.log.Warn("login failed", "request_id", requestID(r.Context()))
		return s.renderAuthForm(w, r, http.StatusUnauthorized, "login", "Invalid password.")
	}

	s.limiter.reset()
	if err := s.startSession(w, r); err != nil {
		return err
	}
	clearCookie(w, preAuthCookie)
	s.log.Info("login succeeded", "request_id", requestID(r.Context()))
	http.Redirect(w, r, "/", http.StatusSeeOther)
	return nil
}

func (s *Server) setupPage(w http.ResponseWriter, r *http.Request) error {
	has, err := s.store.HasUser(r.Context())
	if err != nil {
		return err
	}
	if has {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil
	}
	return s.renderAuthForm(w, r, http.StatusOK, "setup", "")
}

// setupSubmit sets the owner's password the first time the app runs. It is
// only possible while no password exists, and needs the same-origin and
// pre-login token checks, so another web page cannot claim the journal first.
func (s *Server) setupSubmit(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return errBadForm
	}
	if !checkPreAuth(r) {
		return errForbidden
	}
	has, err := s.store.HasUser(r.Context())
	if err != nil {
		return err
	}
	if has {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil
	}

	password := r.PostFormValue("password")
	if err := auth.ValidatePassword(password); err != nil {
		return s.renderAuthForm(w, r, http.StatusUnprocessableEntity, "setup", err.Error()+".")
	}
	if password != r.PostFormValue("confirm") {
		return s.renderAuthForm(w, r, http.StatusUnprocessableEntity, "setup", "The two passwords do not match.")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := s.store.CreateUser(r.Context(), hash); err != nil {
		if errors.Is(err, store.ErrDuplicate) { // lost a race with another request
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return nil
		}
		return err
	}
	if err := s.startSession(w, r); err != nil {
		return err
	}
	clearCookie(w, preAuthCookie)
	s.log.Info("owner password set up", "request_id", requestID(r.Context()))
	http.Redirect(w, r, "/", http.StatusSeeOther)
	return nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.store.DeleteSession(r.Context(), auth.HashToken(c.Value)); err != nil {
			return err
		}
	}
	clearCookie(w, sessionCookie)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
	return nil
}
