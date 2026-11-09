package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/PazVz/mini_coder/internal/auth"
)

func (a *api) createUser(w http.ResponseWriter, r *http.Request) {
	credentials, ok := readJSON[auth.Credentials](w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(userKey{}).(auth.User)
	user, err := a.users.CreateUser(r.Context(), actor.ID, credentials)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (a *api) listUsers(w http.ResponseWriter, r *http.Request) {
	options, ok := readUserPage(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(userKey{}).(auth.User)
	page, err := a.users.ListUsers(r.Context(), actor.ID, options)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *api) updateUserStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := readUserID(w, r)
	if !ok {
		return
	}
	input, ok := readJSON[struct {
		Status string `json:"status"`
	}](w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(userKey{}).(auth.User)
	user, err := a.users.UpdateUserStatus(r.Context(), actor.ID, id, input.Status)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (a *api) updateUserRole(w http.ResponseWriter, r *http.Request) {
	id, ok := readUserID(w, r)
	if !ok {
		return
	}
	input, ok := readJSON[struct {
		Role string `json:"role"`
	}](w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(userKey{}).(auth.User)
	user, err := a.users.UpdateUserRole(r.Context(), actor.ID, id, input.Role)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func readUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeFieldError(w, r, "id", "须为正整数 ID")
		return 0, false
	}
	return id, true
}

func readUserPage(w http.ResponseWriter, r *http.Request) (auth.ListUsersOptions, bool) {
	options := auth.ListUsersOptions{Limit: 50}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeFieldError(w, r, "query", "查询参数格式不合法")
		return options, false
	}
	for field, values := range query {
		if (field != "after_id" && field != "limit") || len(values) != 1 {
			writeFieldError(w, r, "query", "仅接受单个 after_id 和 limit 参数")
			return options, false
		}
	}
	if values, present := query["after_id"]; present {
		afterID, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil {
			writeFieldError(w, r, "after_id", "须为非负整数 ID")
			return options, false
		}
		options.AfterID = afterID
	}
	if values, present := query["limit"]; present {
		limit, err := strconv.ParseInt(values[0], 10, 32)
		if err != nil {
			writeFieldError(w, r, "limit", "须为 1～100 的整数")
			return options, false
		}
		options.Limit = int32(limit)
	}
	return options, true
}

func writeFieldError(w http.ResponseWriter, r *http.Request, field, detail string) {
	writeError(w, r, http.StatusBadRequest, "validation_failed", "请求参数不合法", Validation{
		Field:  field,
		Code:   "invalid_value",
		Detail: detail,
	})
}
