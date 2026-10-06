package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func (a *API) ConsoleListCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.consoleSvc == nil {
		a.errorResponse(w, "Console service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.consoleSvc.GetCommands())
}

func (a *API) ConsoleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.consoleSvc == nil {
		a.errorResponse(w, "Console service unavailable", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Запуск, остановка, перезапуск и переключение ядра из консоли идут под тем же
	// замком, что и «Сервисы»: занят — 409 kernel_op_in_progress, команда не
	// запускается. Информационные команды замок не берут.
	if services.IsKernelLifecycleCommand(req.Command) {
		if !a.tryLifecycleLock(w, r) {
			return
		}
		defer a.lifecycleMu.Unlock()
		// Состояние ядер после команды изменилось: кэши capabilities и активного
		// ядра сбрасываются, иначе панель до 5 с показывает прежнее ядро.
		defer a.ClearCapabilitiesCache()
	}

	result, err := a.consoleSvc.Execute(req.Command)
	// Команды XKeen (-xbr, -mbr, -uk и т. п.) делают манифест слоя недостоверным,
	// в том числе при ненулевом коде выхода (частичное восстановление).
	if !strings.HasPrefix(req.Command, "__diag:") {
		a.layerRequestCheck()
	}
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, result)
}

// layerRequestCheck просит слой «Конфигурация» о внеочередной сверке дрейфа (D-09):
// команды XKeen, терминал и восстановление снимка меняют файлы мимо слоя, и
// сверка раз в минуту показала бы дрейф с опозданием. Безопасен без слоя; при
// выключенном флаге слой запрос не обрабатывает.
func (a *API) layerRequestCheck() {
	if a.configLayer == nil {
		return
	}
	a.configLayer.RequestCheck()
}
