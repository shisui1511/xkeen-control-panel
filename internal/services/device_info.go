package services

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DeviceInfo определяет модель роутера и версию ОС через ndmc (Keenetic OS),
// с ленивой инициализацией и кэшированием результата (ndmc выполняется не
// более одного раза за время жизни процесса).
type DeviceInfo struct {
	mu          sync.Mutex
	initialized bool

	model     string
	osName    string
	osVersion string
}

// NewDeviceInfo создаёт DeviceInfo. Конструктор не выполняет ndmc —
// определение модели/ОС происходит лениво при первом вызове Get().
func NewDeviceInfo() *DeviceInfo {
	return &DeviceInfo{}
}

var (
	ndmcModelRe        = regexp.MustCompile(`(?mi)^\s*model:\s*(.+)$`)
	ndmcTitleRe        = regexp.MustCompile(`(?mi)^\s*title:\s*(\S+)`)
	modelUnsafeCharsRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

// sanitizeModelForHeader повторяет логику install.sh:
//
//	MODEL_RAW -> tr ' ()' '--' -> tr -cd '[:alnum:]._-'
//
// Пробелы и скобки заменяются на '-', затем остаются только safe-символы.
func sanitizeModelForHeader(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(" ", "-", "(", "-", ")", "-")
	s = replacer.Replace(s)
	s = modelUnsafeCharsRe.ReplaceAllString(s, "")
	return s
}

// Get возвращает (model, osName, osVersion). Первый вызов лениво выполняет
// `ndmc -c "show version"` (timeout 3s); последующие вызовы отдают
// закэшированный результат без повторного exec.
func (d *DeviceInfo) Get() (model, osName, osVersion string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.initialized {
		d.detect()
		d.initialized = true
	}
	return d.model, d.osName, d.osVersion
}

func (d *DeviceInfo) detect() {
	// Fallbacks — на случай, если ndmc недоступен (dev-машина, не-Keenetic окружение).
	d.model = "XKeen-Control-Panel"
	d.osName = "Linux"
	d.osVersion = kernelReleaseFallback()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "ndmc", "-c", "show version").Output()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return
	}
	text := string(out)

	d.osName = "Keenetic OS"

	if m := ndmcModelRe.FindStringSubmatch(text); len(m) == 2 {
		if sanitized := sanitizeModelForHeader(m[1]); sanitized != "" {
			d.model = sanitized
		}
	}

	if m := ndmcTitleRe.FindStringSubmatch(text); len(m) == 2 {
		if v := strings.TrimSpace(m[1]); v != "" {
			d.osVersion = v
		}
	}
}

// kernelReleaseFallback возвращает release ядра из /proc/sys/kernel/osrelease
// (портируемо для всех Linux-архитектур сборки: arm64, mipsle, mips), либо
// пустую строку, если файл недоступен.
func kernelReleaseFallback() string {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
