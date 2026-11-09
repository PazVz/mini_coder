package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/PazVz/mini_coder/internal/auth"
)

const (
	sessionCookieName = "minicoder_session"
	maxBodyBytes      = 16 << 10
)

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
	writeJSON(w, http.StatusOK, struct {
		NeedsSetup bool `json:"needs_setup"`
	}{NeedsSetup: needed})
}

func (a *api) setup(w http.ResponseWriter, r *http.Request) {
	credentials, ok := readCredentials(w, r)
	if !ok {
		return
	}
	user, err := a.auth.Setup(r.Context(), r.Header.Get("X-Setup-Token"), credentials)
	if err != nil {
		a.authError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (a *api) login(w http.ResponseWriter, r *http.Request) {
	credentials, ok := readCredentials(w, r)
	if !ok {
		return
	}
	session, err := a.auth.Login(r.Context(), credentials)
	if err != nil {
		a.authError(w, r, err)
		return
	}
	http.SetCookie(w, a.cookie(session.Token, session.ExpiresAt, 24*60*60))
	writeJSON(w, http.StatusOK, session.User)
}

func (a *api) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.auth.Logout(r.Context(), sessionToken(r)); err != nil {
		a.authError(w, r, err)
		return
	}
	http.SetCookie(w, a.cookie("", time.Unix(1, 0), -1))
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) cookie(value string, expiresAt time.Time, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   maxAge,
	}
}

func (a *api) me(w http.ResponseWriter, r *http.Request) {
	// The route uses requireUser, which installs the authenticated user in context.
	user := r.Context().Value(userKey{}).(auth.User)
	writeJSON(w, http.StatusOK, user)
}

func (a *api) authError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *auth.ValidationError
	if errors.As(err, &validation) {
		fields := make([]Validation, 0, len(validation.Fields))
		for _, field := range validation.Fields {
			fields = append(fields, Validation{
				Field:  field.Field,
				Code:   field.Code,
				Detail: field.Detail,
			})
		}
		writeError(w, r, http.StatusBadRequest, "validation_failed", "请求参数不合法", fields...)
		return
	}
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
	case errors.Is(err, auth.ErrUnauthenticated):
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "请重新登录")
	case errors.Is(err, auth.ErrAlreadyInitialized):
		writeError(w, r, http.StatusConflict, "already_initialized", "管理员初始化已完成")
	case errors.Is(err, auth.ErrUsernameTaken):
		writeError(w, r, http.StatusConflict, "username_taken", "用户名已存在")
	case errors.Is(err, auth.ErrSetupDisabled):
		writeError(w, r, http.StatusForbidden, "setup_disabled", "管理员初始化未启用")
	case errors.Is(err, auth.ErrSetupForbidden):
		writeError(w, r, http.StatusForbidden, "forbidden", "初始化令牌无效")
	default:
		a.logger.ErrorContext(r.Context(), "request failed", "request_id", requestID(r), "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "服务暂时无法处理请求")
	}
}

// readCredentials accepts one bounded JSON object and writes an HTTP error on failure.
func readCredentials(w http.ResponseWriter, r *http.Request) (auth.Credentials, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "请求须使用 application/json")
		return auth.Credentials{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	// A pointer distinguishes a null document from a credentials object.
	var credentials *auth.Credentials
	err = decoder.Decode(&credentials)
	if err == nil {
		var extra any
		if nextErr := decoder.Decode(&extra); nextErr != io.EOF {
			if nextErr == nil {
				err = errors.New("multiple JSON values")
			} else {
				err = nextErr
			}
		}
	}
	if err == nil && credentials == nil {
		err = errors.New("null request")
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "request_too_large", "请求体超过 16 KiB")
		} else {
			writeError(w, r, http.StatusBadRequest, "invalid_json", "请求体必须是一个有效的 JSON 对象，且仅包含约定字段")
		}
		return auth.Credentials{}, false
	}
	return *credentials, true
}
