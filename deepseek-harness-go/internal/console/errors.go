package console

import (
	"encoding/json"
	"net/http"
)

// Code 是 console API 的错误码枚举。前端可按 code 做用户提示。
type Code string

const (
	CodeBadRequest    Code = "BAD_REQUEST"
	CodeUnauthorized  Code = "UNAUTHORIZED"
	CodeNotFound      Code = "NOT_FOUND"
	CodeConflict      Code = "CONFLICT"
	CodeUnavailable   Code = "SERVICE_UNAVAILABLE"
	CodeInternal      Code = "INTERNAL"
	CodePluginMissing Code = "PLUGIN_NOT_FOUND"
	CodeModelMissing  Code = "MODEL_NOT_FOUND"
	CodeTaskMissing   Code = "TASK_NOT_FOUND"
	CodeSessionMissing Code = "SESSION_NOT_FOUND"
	CodeApprovalMissing Code = "APPROVAL_NOT_FOUND"
	CodePingFailed    Code = "PING_FAILED"
)

// ErrorBody 是统一的错误响应体。
type ErrorBody struct {
	Error ErrorPayload `json:"error"`
}

// ErrorPayload 是错误明细。
type ErrorPayload struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"traceId,omitempty"`
}

// writeError 以标准格式写出错误。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeErrorCode(w, status, CodeBadRequest, msg)
}

// writeErrorCode 是 writeError 的 code 显式版本。
func writeErrorCode(w http.ResponseWriter, status int, code Code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorBody{Error: ErrorPayload{Code: code, Message: msg}})
}

// writeJSON 以 status 序列化 v 并写出。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// readJSON 解码 r.Body 到 v；出错时返回 false（handler 应写 400）。
func readJSON(r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v) == nil
}

// mapError 把任意 error 映射成 (status, code)。默认 500 + CodeInternal；
// 调用方可对已知 sentinel 做更精确映射。
func mapError(err error) (int, Code) {
	if err == nil {
		return http.StatusOK, ""
	}
	return http.StatusInternalServerError, CodeInternal
}
