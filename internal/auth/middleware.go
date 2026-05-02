package auth

import "net/http"

// SecurityHeaders добавляет заголовки безопасности ко всем ответам
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")
		
		// Prevent MIME sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")
		
		// XSS protection (legacy, but still useful)
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		
		// Content Security Policy
		w.Header().Set("Content-Security-Policy", 
			"default-src 'self'; "+
			"script-src 'self' 'unsafe-inline'; "+
			"style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data:; "+
			"connect-src 'self'; "+
			"font-src 'self'; "+
			"frame-ancestors 'none'")
		
		// Referrer policy
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		
		// Permissions policy
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		
		next.ServeHTTP(w, r)
	})
}
