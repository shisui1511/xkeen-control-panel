package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func (a *API) TrafficQuotaList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.trafficQuotaSvc.ListQuotas())
}

func (a *API) TrafficQuotaGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	q, ok := a.trafficQuotaSvc.GetQuota(id)
	if !ok {
		a.errorResponse(w, "Quota not found", http.StatusNotFound)
		return
	}

	a.jsonResponse(w, q)
}

func (a *API) TrafficQuotaAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	var q services.TrafficQuota
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if q.Name == "" || q.LimitBytes <= 0 {
		a.errorResponse(w, "Name and positive limit_bytes are required", http.StatusBadRequest)
		return
	}
	if q.TargetType == "" {
		q.TargetType = "global"
	}
	if q.Period == "" {
		q.Period = "monthly"
	}

	if err := a.trafficQuotaSvc.AddQuota(&q); err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, q)
}

func (a *API) TrafficQuotaUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	var q services.TrafficQuota
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := a.trafficQuotaSvc.UpdateQuota(id, &q); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	a.jsonResponse(w, q)
}

func (a *API) TrafficQuotaDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	if err := a.trafficQuotaSvc.DeleteQuota(id); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	JSONSuccess(w, nil)
}

func (a *API) TrafficQuotaSetEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	enabled := r.URL.Query().Get("enabled") == "true"
	if err := a.trafficQuotaSvc.SetQuotaEnabled(id, enabled); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	JSONSuccess(w, nil)
}

func (a *API) TrafficQuotaReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	if err := a.trafficQuotaSvc.ResetQuota(id); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	JSONSuccess(w, nil)
}

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

func (a *API) TrafficAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.trafficQuotaSvc.GetAlerts())
}

func (a *API) TrafficAlertsClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	a.trafficQuotaSvc.ClearAlerts()
	JSONSuccess(w, nil)
}

// ConnectionsWebSocket транслирует снимки активных подключений Mihomo
// через WebSocket к браузеру. Данные берутся из fan-out TrafficQuotaService,
// который уже держит одно соединение с Mihomo /connections.
func (a *API) ConnectionsWebSocket(w http.ResponseWriter, r *http.Request) {
	if a.trafficQuotaSvc == nil {
		a.errorResponse(w, "Traffic Quota service unavailable", http.StatusServiceUnavailable)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		return nil
	})

	ch, unsub := a.trafficQuotaSvc.SubscribeConnections()
	defer unsub()

	ctx := r.Context()

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

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		return nil
	})

	ch, unsub := a.trafficQuotaSvc.SubscribeTraffic()
	defer unsub()

	ctx := r.Context()

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
