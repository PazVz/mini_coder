package httpapi

import (
	"net/http"
	"time"
)

const sessionCookieName = "minicoder_session"

func sessionToken(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (a *api) setupStatus(w http.ResponseWriter, r *http.Request) {
	needed, err := a.auth.NeedsSetup(r.Context())
	if err != nil {
		a.authError(w, r, err)
		return
	}
	writeJSON(w, 200, struct {
		NeedsSetup bool `json:"needs_setup"`
	}{needed})
}
func (a *api) setup(w http.ResponseWriter, r *http.Request) {
	c, ok := readCredentials(w, r)
	if !ok {
		return
	}
	u, err := a.auth.Setup(r.Context(), r.Header.Get("X-Setup-Token"), c)
	if err != nil {
		a.authError(w, r, err)
		return
	}
	writeJSON(w, 201, u)
}
func (a *api) login(w http.ResponseWriter, r *http.Request) {
	c, ok := readCredentials(w, r)
	if !ok {
		return
	}
	session, err := a.auth.Login(r.Context(), c)
	if err != nil {
		a.authError(w, r, err)
		return
	}
	http.SetCookie(w, a.cookie(session.Token, session.ExpiresAt, 24*60*60))
	writeJSON(w, 200, session.User)
}
func (a *api) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.auth.Logout(r.Context(), sessionToken(r)); err != nil {
		a.authError(w, r, err)
		return
	}
	http.SetCookie(w, a.cookie("", time.Unix(1, 0), -1))
	w.WriteHeader(http.StatusNoContent)
}
func (a *api) cookie(value string, expires time.Time, maxAge int) *http.Cookie {
	return &http.Cookie{Name: sessionCookieName, Value: value, Path: "/", HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge}
}
