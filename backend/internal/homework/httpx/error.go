// Package httpx 提供与 auth-service 完全一致的错误响应形状：
//
//	{"error": "<稳定机器码>", "message": "<中文>"}
//
// 错误码表见 docs/design/api-contract.md §2。
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// 稳定错误码（api-contract §2）。
const (
	CodeBadRequest            = "bad_request"
	CodeFutureDateForbidden   = "future_date_forbidden"
	CodeUnauthorized          = "unauthorized"
	CodeInvalidToken          = "invalid_token"
	CodeForbidden             = "forbidden"
	CodeNotFound              = "not_found"
	CodeDuplicateSession      = "duplicate_session"
	CodeNoHomeworkDayConflict = "no_homework_day_conflict"
	CodeNotAllTicked          = "not_all_ticked"
	CodeAlreadyCheckedIn      = "already_checked_in"
	CodeSessionHasCheckins    = "session_has_checkins"
	CodeTodoFrozen            = "todo_frozen"
	CodeMemberHasChildren     = "member_has_children"
	CodeLastGuardian          = "last_guardian"
	CodeClassNotEmpty         = "class_not_empty"
	CodeAdminImmutable        = "admin_immutable"
	CodeAlreadyExists         = "already_exists"
	CodeLLMParseFailed        = "llm_parse_failed"
	CodeContentBlocked        = "content_blocked"
	CodeUploadMismatch        = "upload_mismatch"
	CodeLLMUnavailable        = "llm_unavailable"
	CodeWechatUnavailable     = "wechat_unavailable"
	CodeInternal              = "internal_error"
)

type body struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// Write 写出统一错误形状。message 为空时用机器码兜底。
func Write(w http.ResponseWriter, status int, code, message string) {
	if message == "" {
		message = code
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body{Error: code, Message: message}); err != nil {
		slog.Error("write error response", "err", err)
	}
}

// JSON 写出任意 JSON 成功响应。
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response", "err", err)
	}
}
