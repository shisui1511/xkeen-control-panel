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
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
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
	case "switch_kernel":
		kernel := r.URL.Query().Get("kernel")
		out, err = a.xkeenSvc.SwitchKernel(kernel)
	default:
		a.errorResponse(w, a.t(r, "service.invalid_action"), http.StatusBadRequest)
		return
	}

	if err != nil {
		a.errorResponse(w, out, http.StatusInternalServerError)
		return
	}
	w.Write([]byte(out))
}
