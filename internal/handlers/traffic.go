package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

func (a *API) TrafficStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.trafficQuotaSvc.GetStats())
}

// ConnectionsWebSocket транслирует снимки активных подключений Mihomo
// через WebSocket к браузеру. Данные берутся из fan-out TrafficQuotaService,
// который уже держит одно соединение с Mihomo /connections.
func (a *API) ConnectionsWebSocket(w http.ResponseWriter, r *http.Request) {
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	_, ctx, cancel := a.mihomoProxyContext(w, r)
	if cancel == nil {
		return
	}
	defer cancel()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		return nil
	})

	ch, unsub := a.trafficQuotaSvc.SubscribeConnections()
	defer unsub()

	// Ping-горутина
	stopPing := make(chan struct{})
	go func() {
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					return
				}
			case <-stopPing:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	defer close(stopPing)

	// Читаем входящие фреймы (close/ping) — необходимо для корректной работы pong handler
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				conn.Close() // Гарантирует прерывание цикла записи в основном потоке
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case raw, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
				return
			}
		}
	}
}

// TrafficWebSocket транслирует снимки real-time трафика и пиковых нагрузок
// через WebSocket к браузеру. Данные берутся из fan-out TrafficQuotaService.
func (a *API) TrafficWebSocket(w http.ResponseWriter, r *http.Request) {
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	_, ctx, cancel := a.mihomoProxyContext(w, r)
	if cancel == nil {
		return
	}
	defer cancel()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		return nil
	})

	ch, unsub := a.trafficQuotaSvc.SubscribeTraffic()
	defer unsub()

	// Ping-горутина
	stopPing := make(chan struct{})
	go func() {
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					return
				}
			case <-stopPing:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	defer close(stopPing)

	// Читаем входящие фреймы (close/ping) — необходимо для корректной работы pong handler
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				conn.Close() // Гарантирует прерывание цикла записи в основном потоке
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case raw, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
				return
			}
		}
	}
}

// TrafficReset сбрасывает накопленную статистику трафика и пиковых нагрузок.
func (a *API) TrafficReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	if err := a.trafficQuotaSvc.ResetStats(); err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	JSONSuccess(w, nil)
}
