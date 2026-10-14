//go:build router

package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/auth"
	"github.com/shisui1511/xkeen-control-panel/internal/config"
)

// Вход в панель, пока на раздел панели идёт большая запись. Вход писал
// ratelimit.json и sessions.json с fsync прямо в обработчике; на USB-флешке роутера
// под нагрузкой fsync ждёт фиксации журнала и записи чужих данных транзакции — это
// десятки секунд (смоук api-me, прогон 20261014-174446-d4f471f4: вход ответил за 41 с).
// Теперь сессия создаётся в памяти, а файл пишется в фоне. Вердикт — время ответа
// входа и сохранность сессий на диске; если нагрузка не набралась, сценарий
// пропускается (rtWaitWriteback).

const (
	// rtLoginFloor — запас к удвоенному времени входа без нагрузки: предел входа
	// max(2*base+rtLoginFloor, syncTook/4).
	rtLoginFloor = 500 * time.Millisecond
	// rtLoginCount — входов подряд под нагрузкой.
	rtLoginCount = 3
)

func TestRouterLoginWithDirtyDisk(t *testing.T) {
	dir, err := os.MkdirTemp(config.Default().DataDir, "rt-auth-*")
	if err != nil {
		t.Fatalf("каталог в %s: %v", config.Default().DataDir, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("случайный пароль: %v", err)
	}
	pw := base64.RawURLEncoding.EncodeToString(raw)
	hash, err := auth.GeneratePasswordHash(pw)
	if err != nil {
		t.Fatalf("хеш пароля: %v", err)
	}
	svc := auth.NewAuthService(auth.Options{PasswordHash: hash, DataDir: dir})
	t.Cleanup(svc.Stop)
	srv := httptest.NewServer(http.HandlerFunc(svc.HandleLogin))
	t.Cleanup(srv.Close)

	login := func(n int) time.Duration {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"password": pw})
		start := time.Now()
		resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body))
		took := time.Since(start)
		if err != nil {
			t.Fatalf("вход %d: %v", n, err)
		}
		defer resp.Body.Close()
		var out struct {
			CSRFToken string `json:"csrf_token"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != http.StatusOK || out.CSRFToken == "" {
			t.Fatalf("вход %d: http %d, csrf_token пуст: %v", n, resp.StatusCode, out.CSRFToken == "")
		}
		return took
	}

	base := login(0)

	load := rtStartDirtyLoad(t, config.Default().DataDir)
	wb := rtWaitWriteback(t, load.synced)
	t.Logf("в записи на носитель: %d МБ", wb>>20)

	var took [rtLoginCount]time.Duration
	for i := range took {
		took[i] = login(i + 1)
	}

	syncTook, err := load.wait()
	if err != nil {
		t.Fatalf("fsync временного файла: %v", err)
	}
	limit := max(2*base+rtLoginFloor, syncTook/4)
	t.Logf("вход без нагрузки %v; чужие данные записаны за %v, предел входа %v; входы под нагрузкой: %v %v %v",
		base.Round(time.Millisecond), syncTook.Round(time.Millisecond), limit.Round(time.Millisecond),
		took[0].Round(time.Millisecond), took[1].Round(time.Millisecond), took[2].Round(time.Millisecond))
	for i, d := range took {
		if d > limit {
			t.Errorf("вход %d ответил за %v — ждал записи чужих данных на носитель (%v, предел %v)",
				i+1, d.Round(time.Millisecond), syncTook.Round(time.Millisecond), limit.Round(time.Millisecond))
		}
	}

	// Сохранность: фоновая запись обязана довести все сессии до диска.
	want := 1 + rtLoginCount
	deadline := time.Now().Add(30 * time.Second)
	got := -1
	for {
		var f struct {
			Sessions []json.RawMessage `json:"sessions"`
		}
		if b, err := os.ReadFile(filepath.Join(dir, "sessions.json")); err == nil && json.Unmarshal(b, &f) == nil {
			got = len(f.Sessions)
			if got >= want {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Errorf("созданные входом сессии не записаны на диск: %d из %d", got, want)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
