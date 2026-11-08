//go:build router

// router-area: configlayer

package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Предпросмотр «Применить» (D-09…D-12, CFG-02) на настоящей панели устройства:
// GET /api/configlayer/apply/preview после добавления в «Мои узлы» трёх узлов из
// обезличенных ссылок internal/nodes/testdata/links. Вердикт — тело и заголовки
// ответа панели и draft_changes снимка слоя: счёт по сущностям совпадает со
// снимком, учётные данные узлов (UUID, пароль, ключи WireGuard, short-id Reality)
// в ответ не попадают, изменившиеся секреты помечены, ядра идут активным первым.
// POST /api/configlayer/apply с устаревшей ревизией отвечает 409 draft_conflict и
// применение не запускает. Черновик сбрасывается в t.Cleanup; файлы ядер не
// меняются. В сообщения попадают только коды и числа.

const rtPvPath = "/api/configlayer/apply/preview"

// rtPvFile — файл ядра в ответе предпросмотра.
type rtPvFile struct {
	Key            string `json:"key"`
	Action         string `json:"action"`
	Before         string `json:"before"`
	After          string `json:"after"`
	Added          int    `json:"added"`
	Removed        int    `json:"removed"`
	SecretsChanged int    `json:"secrets_changed"`
}

// rtPvView — ответ предпросмотра (поля, которые сверяет сценарий).
type rtPvView struct {
	DraftRevision int64 `json:"draft_revision"`
	Count         int   `json:"count"`
	Changes       []struct {
		Entity string `json:"entity"`
		Kind   string `json:"kind"`
	} `json:"changes"`
	Kernels []struct {
		Kernel   string     `json:"kernel"`
		Active   bool       `json:"active"`
		Deferred bool       `json:"deferred"`
		Files    []rtPvFile `json:"files"`
	} `json:"kernels"`
	Blocked *struct {
		Code  string `json:"code"`
		Count int    `json:"count"`
	} `json:"blocked"`
	SecretsChanged int `json:"secrets_changed"`
}

// rtPvUUID — подстрока вида UUID в теле ответа.
var rtPvUUID = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// rtPvFetch читает предпросмотр с заголовками ответа: сырое тело и разобранные data.
func (e *rtSrc) rtPvFetch() (rtPvView, []byte, http.Header) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := e.p.Stream(ctx, rtPvPath)
	if err != nil {
		e.t.Fatalf("GET %s: %v", rtPvPath, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatalf("GET %s: чтение тела: %v", rtPvPath, err)
	}
	r := e.decode(resp.StatusCode, raw)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", rtPvPath, r.Status, r.Code)
	}
	var view rtPvView
	if err := json.Unmarshal(r.Data, &view); err != nil {
		e.t.Fatalf("GET %s: data не разобрано: %v", rtPvPath, err)
	}
	return view, raw, resp.Header
}

// rtPvUniqueSocks — ссылка socks из фикстуры с паролем, которого нет ни в одном
// применённом узле стенда: её секрет в «станет» обязан быть помечен изменённым.
func rtPvUniqueSocks(t *testing.T) string {
	t.Helper()
	link := rtLinkText(t, "socks")
	n, err := nodes.ParseLink(link)
	if err != nil || n.UUID == "" || n.Password == "" {
		t.Fatalf("фикстура socks не разобрана или без учётных данных: %v", err)
	}
	at := strings.Index(link, "@")
	if at < 0 {
		t.Fatal("в ссылке фикстуры socks нет «@»")
	}
	var salt [6]byte
	if _, err := rand.Read(salt[:]); err != nil {
		t.Fatalf("случайность: %v", err)
	}
	info := base64.StdEncoding.EncodeToString([]byte(n.UUID + ":" + n.Password + "-" + hex.EncodeToString(salt[:])))
	return "socks://" + info + link[at:]
}

// rtPvSecrets — значения учётных данных узлов, разобранные пакетом internal/nodes
// из тех же ссылок, что ушли в черновик. Имя пользователя socks и http (оно лежит
// в UUID узла) секретом не считается и в файлах остаётся видимым.
func rtPvSecrets(t *testing.T, links ...string) []string {
	t.Helper()
	var out []string
	for i, l := range links {
		n, err := nodes.ParseLink(l)
		if err != nil {
			t.Fatalf("ссылка №%d не разобрана: %v", i+1, err)
		}
		if n.Protocol != "socks" && n.Protocol != "http" {
			out = append(out, n.UUID)
		}
		out = append(out, n.Password)
		if n.Reality != nil {
			out = append(out, n.Reality.ShortID, n.Reality.PublicKey)
		}
		if n.WG != nil {
			out = append(out, n.WG.PrivateKey, n.WG.PreSharedKey, n.WG.PeerPublicKey)
		}
	}
	return out
}

func TestRouterApplyPreview(t *testing.T) {
	// Пункт про ядра сверяет активное ядро ветви матрицы.
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()

	// (1) Три узла «Моих узлов»: VLESS Reality, socks с паролем, WireGuard.
	// Порты уникальные: узел не совпадает с уже применёнными на стенде.
	links := []string{
		rtRtUniqueLinkOf(t, "vless_reality", 1),
		rtPvUniqueSocks(t),
		rtRtUniqueLinkOf(t, "wireguard", 2),
	}
	secrets := rtPvSecrets(t, links...)
	for _, l := range links {
		e.rtRstAddMyNode(l)
	}
	draft := e.rtRtDraftChanges()

	// (2) Предпросмотр: счёт по сущностям, маскирование, заголовок.
	view, raw, header := e.rtPvFetch()
	if cc := header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control предпросмотра %q, ожидался no-store", cc)
	}
	if view.Blocked != nil {
		t.Fatalf("предпросмотр заблокирован: код %q, число %d", view.Blocked.Code, view.Blocked.Count)
	}
	if view.Count != len(view.Changes) || view.Count != draft {
		t.Errorf("count %d, строк %d, draft_changes %d: ожидалось равенство", view.Count, len(view.Changes), draft)
	}
	myNodes := 0
	for _, c := range view.Changes {
		if c.Entity == configlayer.EntityMyNode {
			myNodes++
			if c.Kind != configlayer.ChangeAdded {
				t.Errorf("строка узла «Моих узлов»: вид %q, ожидался %q", c.Kind, configlayer.ChangeAdded)
			}
		}
	}
	if myNodes != 3 {
		t.Errorf("строк my_node %d, ожидалось 3", myNodes)
	}
	if rev := e.revision(); view.DraftRevision != rev {
		t.Errorf("draft_revision предпросмотра %d, ревизия черновика %d", view.DraftRevision, rev)
	}
	rtNoSecrets(t, "предпросмотр: учётные данные узлов", raw, secrets...)
	rtNoSecrets(t, "предпросмотр: исходные ссылки", raw, links...)
	if n := len(rtPvUUID.FindAll(raw, -1)); n > 0 {
		t.Errorf("в ответе предпросмотра подстрок вида UUID: %d", n)
	}
	if view.SecretsChanged <= 0 {
		t.Errorf("secrets_changed %d, ожидалось больше 0: новые узлы несут новые секреты", view.SecretsChanged)
	}
	marked, files := false, 0
	for _, k := range view.Kernels {
		for _, f := range k.Files {
			files++
			if strings.Contains(f.After, configlayer.SecretMaskChanged) {
				marked = true
			}
			if f.Action != "write" && f.Action != "delete" {
				t.Errorf("файл предпросмотра: действие %q", f.Action)
			}
		}
	}
	if files == 0 {
		t.Error("в предпросмотре нет файлов ядер, хотя черновик добавляет узлы")
	}
	if !marked {
		t.Error("в «станет»-стороне файлов нет маски изменённого секрета")
	}

	// (3) Ядра: активное первым, остальные — «вступит в силу при запуске».
	if len(view.Kernels) == 0 {
		t.Fatal("в предпросмотре нет ни одного ядра")
	}
	if first := view.Kernels[0]; first.Kernel != tg.Core || !first.Active || first.Deferred {
		t.Errorf("первое ядро %q active=%v deferred=%v, ожидалось активное %q без deferred", first.Kernel, first.Active, first.Deferred, tg.Core)
	}
	for _, k := range view.Kernels[1:] {
		if k.Active || !k.Deferred {
			t.Errorf("ядро %q: active=%v deferred=%v, ожидалось неактивное с deferred", k.Kernel, k.Active, k.Deferred)
		}
	}

	// (4) «Применить» с устаревшей ревизией: 409 draft_conflict, запуска нет.
	stale := e.call(http.MethodPost, "/api/configlayer/apply", map[string]int64{"revision": view.DraftRevision - 1})
	e.rtRtExpect("«Применить» с устаревшей ревизией", stale, http.StatusConflict, "draft_conflict")
	if running, _ := e.rtRstApplyView()["running"].(bool); running {
		t.Error("после отказа draft_conflict применение идёт")
	}
	if got := e.rtRtDraftChanges(); got != draft {
		t.Errorf("после отказа draft_conflict draft_changes %d, было %d", got, draft)
	}
	// (5) Черновик сбрасывает очистка enableLayer.
}
