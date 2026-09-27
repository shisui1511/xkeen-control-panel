package services

import (
	"os"
	"strings"
	"testing"
)

// TestAllowedXrayProtocols_MatchesFrontendList закрепляет набор протоколов,
// для которых Xray пишет outbound во фрагмент подписки (allowedXrayProtocols
// в subscription.go). Единого источника истины между бэкендом и фронтендом
// нет: список продублирован вручную во фронтенде как
// XRAY_SELECTABLE_PROTOCOLS (frontend/src/components/subscriptions/NodeList.svelte,
// IN-03 из код-ревью фазы 133). Тест пинует оба списка: если кто-то добавит
// протокол в allowedXrayProtocols и забудет обновить фронтенд-копию, тест
// упадёт и укажет, что именно рассинхронизировалось, вместо того чтобы
// оставить кнопку выбора узла молча задизейбленной для валидного узла.
func TestAllowedXrayProtocols_MatchesFrontendList(t *testing.T) {
	// Закреплённый снимок бэкенд-списка на момент фикса IN-03. Изменение
	// allowedXrayProtocols в subscription.go без обновления этого списка
	// (и фронтенд-копии) должно ломать тест.
	pinned := map[string]bool{
		"vless":       true,
		"vmess":       true,
		"trojan":      true,
		"shadowsocks": true,
		"socks":       true,
		"http":        true,
		"wireguard":   true,
	}

	if len(allowedXrayProtocols) != len(pinned) {
		t.Fatalf(
			"allowedXrayProtocols changed size (got %d, pinned %d) — sync frontend XRAY_SELECTABLE_PROTOCOLS in frontend/src/components/subscriptions/NodeList.svelte and update this test",
			len(allowedXrayProtocols), len(pinned),
		)
	}
	for proto := range pinned {
		if !allowedXrayProtocols[proto] {
			t.Fatalf(
				"allowedXrayProtocols is missing pinned protocol %q — sync frontend XRAY_SELECTABLE_PROTOCOLS in NodeList.svelte and update this test",
				proto,
			)
		}
	}

	// go test запускается с рабочей директорией пакета — internal/services.
	const frontendListPath = "../../frontend/src/components/subscriptions/NodeList.svelte"
	data, err := os.ReadFile(frontendListPath)
	if err != nil {
		t.Fatalf("read %s: %v", frontendListPath, err)
	}
	content := string(data)

	for proto := range pinned {
		if !strings.Contains(content, "'"+proto+"'") {
			t.Errorf(
				"XRAY_SELECTABLE_PROTOCOLS in %s is missing protocol %q present in backend allowedXrayProtocols",
				frontendListPath, proto,
			)
		}
	}
}
