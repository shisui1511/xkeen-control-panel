package configlayer

import (
	"encoding/json"
	"errors"
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

// Generate выдаёт диагностические файлы.
func (g *DiagGenerator) Generate(src Sections, installed InstalledKernels) ([]GeneratedFile, error) {
	return nil, nil
}

// DiagSectionFor превращает действие диагностики в значение секции черновика.
func DiagSectionFor(action string) (json.RawMessage, error) {
	return nil, nil
}
