package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/PazVz/mini_coder/internal/auth"
)

type Validation struct {
	Field  string `json:"field"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type ErrorResponse struct {
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	Validations []Validation `json:"validations,omitempty"`
	RequestID   string       `json:"request_id"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, fields ...Validation) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, ErrorResponse{code, message, fields, requestID(r)})
}

func (a *api) authError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *auth.ValidationError
	if errors.As(err, &validation) {
		fields := make([]Validation, 0, len(validation.Fields))
		for _, f := range validation.Fields {
			fields = append(fields, Validation{f.Field, f.Code, f.Detail})
		}
		writeError(w, r, 400, "validation_failed", "请求参数不合法", fields...)
		return
	}
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, r, 401, "invalid_credentials", "用户名或密码错误")
	case errors.Is(err, auth.ErrUnauthenticated):
		writeError(w, r, 401, "unauthenticated", "请重新登录")
	case errors.Is(err, auth.ErrAlreadyInitialized):
		writeError(w, r, 409, "already_initialized", "管理员初始化已完成")
	case errors.Is(err, auth.ErrUsernameTaken):
		writeError(w, r, 409, "username_taken", "用户名已存在")
	case errors.Is(err, auth.ErrSetupDisabled):
		writeError(w, r, 403, "setup_disabled", "管理员初始化未启用")
	case errors.Is(err, auth.ErrSetupForbidden):
		writeError(w, r, 403, "forbidden", "初始化令牌无效")
	default:
		a.logger.ErrorContext(r.Context(), "request failed", "request_id", requestID(r), "error", err)
		writeError(w, r, 500, "internal_error", "服务暂时无法处理请求")
	}
}

const maxBodyBytes = 16 << 10

func readCredentials(w http.ResponseWriter, r *http.Request) (auth.Credentials, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, r, 415, "unsupported_media_type", "请求须使用 application/json")
		return auth.Credentials{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	// A pointer distinguishes a null document from a credentials object.
	var c *auth.Credentials
	err = decoder.Decode(&c)
	if err == nil {
		var extra any
		if next := decoder.Decode(&extra); next != io.EOF {
			if next == nil {
				err = errors.New("multiple JSON values")
			} else {
				err = next
			}
		}
	}
	if err == nil && c == nil {
		err = errors.New("null request")
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, r, 413, "request_too_large", "请求体超过 16 KiB")
		} else {
			writeError(w, r, 400, "invalid_json", "请求体必须是一个有效的 JSON 对象，且仅包含约定字段")
		}
		return auth.Credentials{}, false
	}
	return *c, true
}
