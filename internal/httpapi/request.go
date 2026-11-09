package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

// readJSON accepts one bounded JSON object and writes an HTTP error on failure.
func readJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var zero T
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "请求须使用 application/json")
		return zero, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	// A pointer distinguishes a null document from a request object.
	var value *T
	err = decoder.Decode(&value)
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
	if err == nil && value == nil {
		err = errors.New("null request")
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "request_too_large", "请求体超过 16 KiB")
		} else {
			writeError(w, r, http.StatusBadRequest, "invalid_json", "请求体必须是一个有效的 JSON 对象，且仅包含约定字段")
		}
		return zero, false
	}
	return *value, true
}
