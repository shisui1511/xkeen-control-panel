package handlers

import "net/http"

func (a *API) ServiceStatus(w http.ResponseWriter, r *http.Request) {
	out, err := a.xkeenSvc.Status()
	if err != nil {
		a.errorResponse(w, out, http.StatusInternalServerError)
		return
	}
	w.Write([]byte(out))
}

func (a *API) ServiceControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	action := r.URL.Query().Get("action")

	var out string
	var err error

	switch action {
	case "start":
		out, err = a.xkeenSvc.Start()
	case "stop":
		out, err = a.xkeenSvc.Stop()
	case "restart":
		out, err = a.xkeenSvc.Restart()
	default:
		a.errorResponse(w, "Invalid action", http.StatusBadRequest)
		return
	}

	if err != nil {
		a.errorResponse(w, out, http.StatusInternalServerError)
		return
	}
	w.Write([]byte(out))
}
