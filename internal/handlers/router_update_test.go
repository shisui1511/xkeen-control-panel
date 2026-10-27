//go:build router

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Проверка обновления панели на настоящей панели устройства. Источник истины —
// реальные релизы GitHub (published_at и блок xcp-release первой строки тела),
// прочитанные тестом отдельно, и часы устройства. Сеть и время не подменяются:
// оракул пишется здесь заново по правилам модели выпусков и функций продукта
// не вызывает. Состояние автоустановки зависит от сборки панели: на сборке
// ветки (версия вида X.Y.Z-alpha.N+g<sha>) это dev_build, на сборке под номером
// вышедшего релиза — delayed, scheduled или held. Подтест второго уровня назван
// ожидаемым состоянием, поэтому в results.tsv видно, какое состояние
// наблюдалось. Канал и настройки автообновления возвращаются прежними.

// rtUpdEnv — обёртка ответа панели.
type rtUpdEnv[T any] struct {
	Success bool   `json:"success"`
	Data    T      `json:"data"`
	Error   string `json:"error"`
	Code    string `json:"code"`
}

// rtUpdState — поля GET /api/update/state, нужные сценариям.
type rtUpdState struct {
	Channel           string `json:"channel"`
	CurrentVersion    string `json:"current_version"`
	LatestVersion     string `json:"latest_version"`
	HasUpdate         bool   `json:"has_update"`
	AutoCheck         bool   `json:"auto_check"`
	AutoInstall       bool   `json:"auto_install"`
	InstallWindow     string `json:"install_window"`
	FailedVersion     string `json:"auto_failed_version"`
	LatestPublishedAt string `json:"latest_published_at"`
	LatestUrgent      bool   `json:"latest_urgent"`
	LatestHold        bool   `json:"latest_hold"`
	AutoState         string `json:"auto_state"`
	AutoInstallOn     string `json:"auto_install_on"`
}

// rtUpdGet — GET с разбором data ответа; любой статус кроме 200 — провал.
func rtUpdGet[T any](t *testing.T, p *routertest.Panel, path string) T {
	t.Helper()
	var env rtUpdEnv[T]
	code, err := p.GetJSON(path, &env)
	if err != nil || code != http.StatusOK {
		t.Fatalf("GET %s: статус %d, ошибка %v", path, code, err)
	}
	return env.Data
}

// rtUpdPost — POST, ожидается 200.
func rtUpdPost(t *testing.T, p *routertest.Panel, path string, body any) {
	t.Helper()
	code, raw, err := p.Call(http.MethodPost, path, body)
	if err != nil || code != http.StatusOK {
		t.Fatalf("POST %s: статус %d, ошибка %v, тело %s", path, code, err, string(raw))
	}
}

func rtUpdReadState(t *testing.T, p *routertest.Panel) rtUpdState {
	t.Helper()
	return rtUpdGet[rtUpdState](t, p, "/api/update/state")
}

// rtUpdIsAlpha — сборка разработки по номеру версии (независимо от продукта).
func rtUpdIsAlpha(version string) bool {
	return strings.Contains(strings.SplitN(version, "+", 2)[0], "-alpha")
}

// rtUpdRelease — релиз GitHub, прочитанный устройством напрямую.
type rtUpdRelease struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
}

// rtUpdReleases читает список релизов; второе значение — причина недоступности
// GitHub (лимит 403, сеть, 5xx), пустая при успехе.
func rtUpdReleases() ([]rtUpdRelease, string) {
	resp, err := utils.SafeHTTPClient(20 * time.Second).Get(githubAPIReleases + "?per_page=30")
	if err != nil {
		return nil, "GitHub недоступен: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "GitHub недоступен: статус " + resp.Status
	}
	var rels []rtUpdRelease
	if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
		return nil, "GitHub: ответ не разобран: " + err.Error()
	}
	return rels, ""
}

// rtUpdMarkerRe — блок xcp-release первой строки тела (формат — шапка
// .github/scripts/auto_release.sh), своё выражение теста.
var rtUpdMarkerRe = regexp.MustCompile(`^<!-- xcp-release: (\{.*\}) -->$`)

// rtUpdBlock возвращает значения блока из первой строки тела; нет блока или он
// битый — false и false.
func rtUpdBlock(body string) (urgent, hold bool) {
	first := strings.SplitN(body, "\n", 2)[0]
	first = strings.TrimSuffix(first, "\r")
	m := rtUpdMarkerRe.FindStringSubmatch(first)
	if m == nil {
		return false, false
	}
	var v struct {
		Urgent *bool `json:"urgent"`
		Hold   *bool `json:"hold"`
	}
	if json.Unmarshal([]byte(m[1]), &v) != nil || v.Urgent == nil || v.Hold == nil {
		return false, false
	}
	return *v.Urgent, *v.Hold
}

// rtUpdWindow подбирает окно установки, не содержащее текущий момент и не
// переходящее через полночь, чтобы фоновая установка во время теста не началась.
// Возвращает окно и конец окна в минутах от полуночи.
func rtUpdWindow(now time.Time) (window string, endMin int) {
	h := now.Hour()
	if h <= 20 {
		return fmt.Sprintf("%02d:00-%02d:00", h+2, h+3), (h + 3) * 60
	}
	return fmt.Sprintf("%02d:00-%02d:00", h-3, h-2), (h - 2) * 60
}

// rtUpdWindowDate — дата ближайшего окна [.., endMin) для момента from.
func rtUpdWindowDate(from time.Time, endMin int) string {
	if from.Hour()*60+from.Minute() < endMin {
		return from.Format("2006-01-02")
	}
	return from.AddDate(0, 0, 1).Format("2006-01-02")
}

// rtUpdRestore запоминает канал и настройки автообновления и возвращает их в
// t.Cleanup.
func rtUpdRestore(t *testing.T, p *routertest.Panel) {
	t.Helper()
	ch := rtUpdGet[map[string]string](t, p, "/api/update/channel")["channel"]
	st := rtUpdReadState(t, p)
	t.Cleanup(func() {
		if ch == "stable" || ch == "beta" {
			if code, _, err := p.Call(http.MethodPost, "/api/update/channel", map[string]string{"channel": ch}); err != nil || code != http.StatusOK {
				t.Errorf("канал обновлений не возвращён: статус %d, ошибка %v", code, err)
			}
		}
		body := map[string]any{"auto_check": st.AutoCheck, "auto_install": st.AutoInstall, "install_window": st.InstallWindow}
		if code, _, err := p.Call(http.MethodPost, "/api/update/settings", body); err != nil || code != http.StatusOK {
			t.Errorf("настройки автообновления не возвращены: статус %d, ошибка %v", code, err)
		}
	})
}

// TestRouterUpdateAutoState сверяет auto_state и auto_install_on панели с реальными
// релизами GitHub и часами устройства для обоих каналов.
func TestRouterUpdateAutoState(t *testing.T) {
	utils.ApplySystemTimezone()
	p := routertest.NewPanel(t)
	rtUpdRestore(t, p)

	for _, channel := range []string{"stable", "beta"} {
		t.Run(channel, func(t *testing.T) {
			rtUpdAutoStateChannel(t, p, channel)
		})
	}
}

func rtUpdAutoStateChannel(t *testing.T, p *routertest.Panel, channel string) {
	// Если между первым и последним чтением сменились местные сутки, расчёт
	// повторяется один раз.
	for attempt := 0; attempt < 2; attempt++ {
		started := time.Now()
		window, endMin := rtUpdWindow(started)
		rtUpdPost(t, p, "/api/update/channel", map[string]string{"channel": channel})
		rtUpdPost(t, p, "/api/update/settings", map[string]any{"auto_install": true, "install_window": window})

		// Проверка записывает факты релиза в планировщик; сбой GitHub — не провал продукта.
		checkCode, _, err := p.Call(http.MethodGet, "/api/update/check?channel="+channel, nil)
		checkOK := err == nil && checkCode == http.StatusOK
		st := rtUpdReadState(t, p)
		now := time.Now()
		if now.Format("2006-01-02") != started.Format("2006-01-02") && attempt == 0 {
			continue
		}

		alpha := rtUpdIsAlpha(st.CurrentVersion)
		want, wantDate := "", ""
		var rels []rtUpdRelease
		if checkOK {
			var reason string
			rels, reason = rtUpdReleases()
			if reason != "" {
				checkOK = false
				t.Logf("%s", reason)
			}
		}
		switch {
		case alpha:
			want = "dev_build"
		case !checkOK:
			t.Skipf("состояние на сборке выпуска сверяется по данным GitHub, а они недоступны (проверка панели: статус %d)", checkCode)
		}

		if checkOK {
			var rel *rtUpdRelease
			for i := range rels {
				if rels[i].TagName == "v"+st.LatestVersion {
					rel = &rels[i]
					break
				}
			}
			if rel == nil {
				t.Errorf("релиз %q из состояния панели не найден в списке GitHub", st.LatestVersion)
			} else {
				urgent, hold := rtUpdBlock(rel.Body)
				if st.LatestPublishedAt != rel.PublishedAt {
					t.Errorf("latest_published_at: панель %q, GitHub %q", st.LatestPublishedAt, rel.PublishedAt)
				}
				if st.LatestUrgent != urgent || st.LatestHold != hold {
					t.Errorf("факты блока релиза %s: панель urgent=%v hold=%v, GitHub urgent=%v hold=%v",
						st.LatestVersion, st.LatestUrgent, st.LatestHold, urgent, hold)
				}
				if !alpha {
					want, wantDate = rtUpdExpect(st, rel, urgent, hold, channel, now, endMin)
				}
			}
		}
		if want == "" {
			t.Fatalf("ожидаемое состояние не определено (канал %s, версия %s)", channel, st.CurrentVersion)
		}

		t.Run(want, func(t *testing.T) {
			if st.AutoState != want || st.AutoInstallOn != wantDate {
				t.Errorf("канал %s: auto_state=%q auto_install_on=%q, ожидалось %q и %q (has_update=%v, latest=%s, published_at=%s, urgent=%v, hold=%v, окно %s)",
					channel, st.AutoState, st.AutoInstallOn, want, wantDate, st.HasUpdate, st.LatestVersion,
					st.LatestPublishedAt, st.LatestUrgent, st.LatestHold, window)
			}
		})

		// Автоустановка выключена: состояние none (у сборки разработки — по-прежнему dev_build).
		rtUpdPost(t, p, "/api/update/settings", map[string]any{"auto_install": false})
		off := rtUpdReadState(t, p)
		wantOff := "none"
		if alpha {
			wantOff = "dev_build"
		}
		if off.AutoState != wantOff || off.AutoInstallOn != "" {
			t.Errorf("канал %s, автоустановка выключена: auto_state=%q auto_install_on=%q, ожидалось %q без даты",
				channel, off.AutoState, off.AutoInstallOn, wantOff)
		}
		return
	}
}

// rtUpdExpect — ожидаемое состояние и дата на сборке выпуска (не alpha) по
// правилам модели: сначала нет обновления, затем неудачная версия, стоп, срочное,
// Основной канал выдерживает релиз 168 часов, иначе ближайшее окно.
func rtUpdExpect(st rtUpdState, rel *rtUpdRelease, urgent, hold bool, channel string, now time.Time, endMin int) (state, date string) {
	dateFrom := func(from time.Time) string { return rtUpdWindowDate(from, endMin) }
	switch {
	case !st.HasUpdate:
		return "none", ""
	case st.FailedVersion != "" && st.FailedVersion == st.LatestVersion:
		return "failed_version", ""
	case hold:
		return "held", ""
	case urgent:
		return "urgent", dateFrom(now)
	}
	if channel == "stable" {
		published, err := time.Parse(time.RFC3339, rel.PublishedAt)
		if err != nil {
			return "none", ""
		}
		if due := published.Add(168 * time.Hour); due.After(now) {
			return "delayed", dateFrom(due.In(time.Local))
		}
	}
	return "scheduled", dateFrom(now)
}
