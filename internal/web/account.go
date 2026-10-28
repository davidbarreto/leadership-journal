package web

import (
	"fmt"
	"net/http"

	"leadership-journal/internal/auth"
)

func (s *Server) registerAccount(rt router) {
	rt.protected("POST /settings/password", s.settingsPassword)
	rt.protected("POST /settings/backup", s.settingsBackup)
}

// settingsPassword changes the owner's password. It asks for the current one
// and shares the login rate limit, so this form cannot be used to guess it.
func (s *Server) settingsPassword(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	if !s.limiter.allowed() {
		return userErrorf("Too many failed attempts. Try again in a few minutes")
	}
	hash, err := s.store.PasswordHash(ctx)
	if err != nil {
		return err
	}
	ok, err := auth.VerifyPassword(r.PostFormValue("current"), hash)
	if err != nil {
		return err
	}
	if !ok {
		s.limiter.recordFailure()
		s.log.Warn("password change refused: wrong current password", "request_id", requestID(ctx))
		return userErrorf("The current password is not right")
	}

	next := r.PostFormValue("password")
	if err := auth.ValidatePassword(next); err != nil {
		return userErrorf(err.Error())
	}
	if next != r.PostFormValue("confirm") {
		return userErrorf("The two passwords do not match")
	}
	newHash, err := auth.HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.store.UpdatePasswordHash(ctx, newHash); err != nil {
		return err
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.store.DeleteOtherSessions(ctx, auth.HashToken(c.Value)); err != nil {
			return err
		}
	}
	s.limiter.reset()
	s.log.Info("password changed", "request_id", requestID(ctx))
	return s.done(w, r, "/settings", "Password changed. Other sessions were logged out.")
}

func (s *Server) settingsBackup(w http.ResponseWriter, r *http.Request) error {
	path, err := s.store.Backup(r.Context())
	if err != nil {
		return err
	}
	s.log.Info("backup written", "path", path, "request_id", requestID(r.Context()))
	return s.done(w, r, "/settings", fmt.Sprintf("Backup written to %s. It holds everything, so keep it as private as the journal itself.", path))
}
