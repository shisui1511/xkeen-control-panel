package configlayer

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Диагностическая секция черновика и имена её файлов (D-19).
const (
	DiagSection   = "diag"
	DiagXrayRel   = "04_outbounds.xcp-diag.tail.json"
	DiagMihomoRel = "proxy_providers/xcp-diag.yaml"
)

// ErrUnknownDiagAction — действие диагностики не из списка.
var ErrUnknownDiagAction = errors.New("неизвестное диагностическое действие")

// DiagConfig — содержимое секции diag черновика.
type DiagConfig struct {
	Enabled      bool `json:"enabled"`
	BrokenXray   bool `json:"broken_xray"`
	BrokenMihomo bool `json:"broken_mihomo"`
}

// DiagGenerator — безвредный диагностический генератор (D-19).
type DiagGenerator struct {
	devMode func() bool
}

// NewDiagGenerator создаёт генератор; devMode опрашивается при каждой сборке.
func NewDiagGenerator(devMode func() bool) *DiagGenerator {
	return &DiagGenerator{devMode: devMode}
}

// ID — идентификатор генератора в реестре.
func (g *DiagGenerator) ID() string { return DiagSection }

// Заведомо безвредное и заведомо битое содержимое диагностических файлов.
const (
	diagXrayOK             = "{}\n"
	diagXrayBroken         = `{"outbounds":[{"protocol":"nope","tag":"xcp-diag-broken"}]}` + "\n"
	diagMihomoBroken       = "proxies: [unclosed\n"
	diagMihomoProvider     = "proxies:\n  - name: xcp-diag\n    type: socks5\n    server: 127.0.0.1\n    port: 1\n"
	diagActionAdd          = "add"
	diagActionBrokenXray   = "add_broken_xray"
	diagActionBrokenMihomo = "add_broken_mihomo"
	diagActionRemove       = "remove"
)

// Generate выдаёт диагностические файлы. Генератор зарегистрирован всегда, но
// вне dev_mode, без секции diag или при Enabled=false не выдаёт ничего: прод-набор
// файлов пуст (D-19), а переключение dev_mode не требует перезапуска панели.
// Файлы ядер, которых нет в installed, отбрасывает реестр.
func (g *DiagGenerator) Generate(src Sections, _ InstalledKernels) ([]GeneratedFile, error) {
	if g.devMode == nil || !g.devMode() {
		return nil, nil
	}
	raw, ok := src[DiagSection]
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	var cfg DiagConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("секция %s: %w", DiagSection, err)
	}
	if !cfg.Enabled {
		return nil, nil
	}
	xray, mihomo := diagXrayOK, diagMihomoProvider
	if cfg.BrokenXray {
		xray = diagXrayBroken
	}
	if cfg.BrokenMihomo {
		mihomo = diagMihomoBroken
	}
	return []GeneratedFile{
		{Kernel: KernelXray, RelPath: DiagXrayRel, Kind: KindXrayJSON, Content: []byte(xray)},
		{Kernel: KernelMihomo, RelPath: DiagMihomoRel, Kind: KindMihomoProxyProvider, Content: []byte(mihomo)},
	}, nil
}

// DiagSectionFor превращает действие диагностики в значение секции черновика:
// "add", "add_broken_xray", "add_broken_mihomo" или "remove" (nil — секция
// удаляется). Любое другое действие — ErrUnknownDiagAction.
func DiagSectionFor(action string) (json.RawMessage, error) {
	var cfg DiagConfig
	switch action {
	case diagActionAdd:
		cfg.Enabled = true
	case diagActionBrokenXray:
		cfg.Enabled, cfg.BrokenXray = true, true
	case diagActionBrokenMihomo:
		cfg.Enabled, cfg.BrokenMihomo = true, true
	case diagActionRemove:
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownDiagAction, action)
	}
	return json.Marshal(cfg)
}
