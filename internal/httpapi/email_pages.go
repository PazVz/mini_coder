package httpapi

import (
	_ "embed"
	"io"
	"net/http"
)

//go:embed email_pages/register.html
var registerHTML string

//go:embed email_pages/confirm.html
var confirmHTML string

//go:embed email_pages/email.js
var emailJS string

//go:embed email_pages/email.css
var emailCSS string

func registrationPage(w http.ResponseWriter, r *http.Request) {
	writeEmailPage(w, r, "text/html; charset=utf-8", registerHTML)
}

func emailConfirmationPage(w http.ResponseWriter, r *http.Request) {
	writeEmailPage(w, r, "text/html; charset=utf-8", confirmHTML)
}

func emailScript(w http.ResponseWriter, r *http.Request) {
	writeEmailPage(w, r, "text/javascript; charset=utf-8", emailJS)
}

func emailStyle(w http.ResponseWriter, r *http.Request) {
	writeEmailPage(w, r, "text/css; charset=utf-8", emailCSS)
}

func writeEmailPage(w http.ResponseWriter, r *http.Request, contentType, body string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, body)
	}
}
