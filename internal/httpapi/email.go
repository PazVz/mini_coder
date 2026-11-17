package httpapi

import (
	"net/http"

	"github.com/PazVz/mini_coder/internal/auth"
)

func (a *api) requireEmails(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.emails == nil {
			a.serviceError(w, r, auth.ErrEmailUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *api) requestRegistration(w http.ResponseWriter, r *http.Request) {
	input, ok := readJSON[struct {
		Email string `json:"email"`
	}](w, r)
	if !ok {
		return
	}
	if err := a.emails.RequestRegistration(r.Context(), input.Email); err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeEmailAccepted(w)
}

func (a *api) register(w http.ResponseWriter, r *http.Request) {
	input, ok := readJSON[auth.RegistrationInput](w, r)
	if !ok {
		return
	}
	user, err := a.emails.Register(r.Context(), input)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (a *api) requestEmailVerification(w http.ResponseWriter, r *http.Request) {
	if _, ok := readJSON[struct{}](w, r); !ok {
		return
	}
	user := r.Context().Value(userKey{}).(auth.User)
	if err := a.emails.RequestEmailVerification(r.Context(), user.ID, sessionToken(r)); err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeEmailAccepted(w)
}

func (a *api) requestEmailChange(w http.ResponseWriter, r *http.Request) {
	input, ok := readJSON[auth.EmailChange](w, r)
	if !ok {
		return
	}
	user := r.Context().Value(userKey{}).(auth.User)
	if err := a.emails.RequestEmailChange(r.Context(), user.ID, sessionToken(r), input); err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeEmailAccepted(w)
}

func (a *api) confirmEmail(w http.ResponseWriter, r *http.Request) {
	input, ok := readJSON[struct {
		Token string `json:"token"`
	}](w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(userKey{}).(auth.User)
	user, err := a.emails.ConfirmEmail(r.Context(), actor.ID, sessionToken(r), input.Token)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func writeEmailAccepted(w http.ResponseWriter) {
	writeJSON(w, http.StatusAccepted, struct {
		Message string `json:"message"`
	}{Message: "验证邮件已提交，请查收邮件；重新申请后请使用最新链接。"})
}
