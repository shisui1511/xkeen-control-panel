package handlers

import (
	"encoding/json"
	"net/http"
)

// APIResponse is the unified envelope for all handler responses.
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	// Code — машиночитаемая причина ошибки (session_is_current,
	// session_not_found, session_ttl_out_of_range, …), в дополнение к
	// человекочитаемому Error — фронтенд различает конкретные отказы без
	// парсинга текста сообщения.
	Code string `json:"code,omitempty"`
	// Detail — технический текст ошибки (ошибка ФС, сети, парсера), отдельно
	// от переведённого Error: перевод остаётся на языке запроса, а деталь
	// фронтенд показывает под ним как есть.
	Detail string `json:"detail,omitempty"`
}

// JSONSuccess writes a successful JSON response with the given data payload.
func JSONSuccess(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIResponse{Success: true, Data: data})
}

// JSONError writes a failure JSON response with the given HTTP status code and error message.
// Сообщение не экранируется как HTML: фронтенд выводит его текстом, а
// encoding/json и так кодирует <, >, & как \u003c…; иначе вывод валидатора
// показывался бы с сущностями вида &#34;.
func JSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(APIResponse{Success: false, Error: msg})
}

// JSONErrorCode is like JSONError but also sets a machine-readable Code
// alongside the human-readable message (e.g. "session_is_current",
// "session_not_found", "session_ttl_out_of_range").
func JSONErrorCode(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIResponse{Success: false, Error: msg, Code: code})
}

// JSONErrorCodeDetail is like JSONErrorCode but also carries the technical
// error text in Detail, kept apart from the translated msg.
func JSONErrorCodeDetail(w http.ResponseWriter, status int, code, msg, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIResponse{Success: false, Error: msg, Code: code, Detail: detail})
}
