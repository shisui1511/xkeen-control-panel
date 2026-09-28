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
		//
		// script-src намеренно без 'unsafe-inline' (134-REVIEW WR-01):
		// единственный inline-скрипт (регистрация service worker) вынесен в
		// frontend/src/sw-register.ts и подключается как обычный
		// module-скрипт (frontend/index.html), уже покрытый 'self'.
		// 'unsafe-inline' отключал бы основную анти-XSS защиту CSP для
		// всего приложения.
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"img-src 'self' data: https://cdn.jsdelivr.net https://raw.githubusercontent.com https://github.com https://www.redditstatic.com https://www.svgrepo.com; "+
				"connect-src 'self' https://ipinfo.io https://*.ipinfo.io https://api.ipify.org https://api64.ipify.org https://icanhazip.com https://*.icanhazip.com https://ipapi.co https://api.my-ip.io; "+
				"font-src 'self' https://fonts.gstatic.com; "+
				"frame-ancestors 'none'")

		// Referrer policy
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Permissions policy
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		// HSTS: не включаем (D-16). Панель использует самоподписанный (или
		// пользовательский собственный) сертификат — если браузер запомнит
		// положительный max-age, при следующем недоверенном сертификате
		// Chrome не даст пройти предупреждение и заблокирует доступ к
		// панели без обхода через flags/chrome://net-internals. max-age=0
		// не включает HSTS, но снимает политику, закэшированную версиями
		// панели до этого плана (RFC 6797 §6.1.1).
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=0")
		}

		next.ServeHTTP(w, r)
	})
}
