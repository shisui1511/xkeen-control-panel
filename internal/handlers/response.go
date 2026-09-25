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
