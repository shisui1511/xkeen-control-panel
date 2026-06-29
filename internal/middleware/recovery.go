package middleware

import (
	"encoding/json"
	"log"
	"net/http"
)

// Recovery is an HTTP middleware that catches any panics during request processing and returns a 500 Internal Server Error.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("Panic recovered: %v", rec)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"error":   "Internal Server Error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
