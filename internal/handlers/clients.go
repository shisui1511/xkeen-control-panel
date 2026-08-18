package handlers

import (
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// SystemClients отдает список сетевых клиентов локальной сети с сопоставлением IP -> имя устройства.
// GET /api/system/clients
func (a *API) SystemClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var clients map[string]services.ClientInfo
	if a.clientResolver != nil {
		clients = a.clientResolver.GetClients()
	} else {
		clients = make(map[string]services.ClientInfo)
	}

	a.jsonResponse(w, map[string]any{
		"clients": clients,
	})
}
