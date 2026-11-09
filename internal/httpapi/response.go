package httpapi

import (
	"encoding/json"
	"net/http"
)

// Validation identifies a rejected request field and its validation rule.
type Validation struct {
	Field  string `json:"field"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// ErrorResponse is the public failure contract shared by handlers and middleware.
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
	writeJSON(w, status, ErrorResponse{
		Code:        code,
		Message:     message,
		Validations: fields,
		RequestID:   requestID(r),
	})
}
