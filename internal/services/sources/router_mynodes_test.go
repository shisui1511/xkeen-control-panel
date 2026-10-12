//go:build router

package sources

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Правка «Моих узлов» по исходному тексту и дубли предпросмотра на настоящем
// слое. Вердикт — секция черновика, файлы генератора и ответы сервиса.

// my — секция src_my черновика.
func (e *rtEnv) my(t *testing.T) MySourceConfig {
	t.Helper()
	cfg, _, err := e.svc.draftMy()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func rtAsSourceError(err error) *nodes.SourceError {
	var se *nodes.SourceError
	if errors.As(err, &se) {
		return se
	}
	return nil
}

func TestRouterMyNodesEditKeepsID(t *testing.T) {
	e := rtNewEnv(t, false, false)
	e.installed = rtBoth
	ctx := context.Background()
	linkA := rtLink(t, "vless_ws")
	linkB := strings.Replace(linkA, rtUUIDA, rtUUIDB, 1)

	if _, err := e.svc.SaveNode(e.rev(t), "", "Первый", linkA); err != nil {
		t.Fatalf("SaveNode (новая запись): %v", err)
	}
	cfg := e.my(t)
	if len(cfg.Entries) != 1 || cfg.Entries[0].Name != "Первый" || cfg.Entries[0].Raw != linkA {
		t.Fatalf("новая запись: %+v", cfg.Entries)
	}
	id := cfg.Entries[0].ID
	tagBefore := rtXrayTags(t, rtGenerate(t, e.svc, kernelXray, e.draftInput(t), rtBoth))

	keyA, err := e.svc.PreviewNode(ctx, linkA, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SaveNode(e.rev(t), id, "Новое имя", linkB); err != nil {
		t.Fatalf("SaveNode (правка): %v", err)
	}
	cfg = e.my(t)
	if len(cfg.Entries) != 1 || cfg.Entries[0].ID != id || cfg.Entries[0].Name != "Новое имя" || cfg.Entries[0].Raw != linkB {
		t.Fatalf("запись после правки: %+v", cfg.Entries)
	}
	// Тег в файле Xray прежний, ключ узла другой, имя в Mihomo — новое.
	tagAfter := rtXrayTags(t, rtGenerate(t, e.svc, kernelXray, e.draftInput(t), rtBoth))
	if len(tagBefore) != 1 || len(tagAfter) != 1 || tagBefore[0] != tagAfter[0] || tagAfter[0] != "xcp-my-"+id {
		t.Errorf("тег до %v и после %v правки должны совпадать и быть xcp-my-%s", tagBefore, tagAfter, id)
	}
	keyB, err := e.svc.PreviewNode(ctx, linkB, id)
	if err != nil {
		t.Fatal(err)
	}
	if keyA.MyNodes.Nodes[0].Key == keyB.MyNodes.Nodes[0].Key {
		t.Error("ключ узла не изменился при смене UUID")
	}
	if names := rtNames(rtMihomoProxies(t, rtGenerate(t, e.svc, kernelMihomo, e.draftInput(t), rtBoth))); len(names) != 1 || names[0] != "Новое имя" {
		t.Errorf("имя в файле Mihomo: %v", names)
	}
	// Пустое имя при сохранении — имя узла из текста.
	if _, err := e.svc.SaveNode(e.rev(t), id, "", linkB); err != nil {
		t.Fatal(err)
	}
	if got := e.my(t).Entries[0].Name; got != "VLESS WS" {
		t.Errorf("имя при пустом: %q", got)
	}
}

func TestRouterMyNodesEditSingleNode(t *testing.T) {
	e := rtNewEnv(t, false, false)
	e.installed = rtBoth
	linkA, linkB := rtLink(t, "vless_ws"), rtLink(t, "vless_reality")
	if _, err := e.svc.SaveNode(e.rev(t), "", "", linkA); err != nil {
		t.Fatal(err)
	}
	id := e.my(t).Entries[0].ID

	for name, text := range map[string]string{"два узла": linkA + "\n" + linkB, "пусто": "", "не узел": "foo://x"} {
		_, err := e.svc.SaveNode(e.rev(t), id, "", text)
		if se := rtAsSourceError(err); se == nil || se.Code != nodes.CodeEditSingleNode {
			t.Errorf("%s: ошибка %v, нужна edit_single_node", name, err)
		}
		view, err := e.svc.PreviewNode(context.Background(), text, id)
		if se := rtAsSourceError(err); se == nil || se.Code != nodes.CodeEditSingleNode {
			t.Errorf("%s: предпросмотр вернул %v, нужна edit_single_node", name, err)
		}
		if view.Errors == nil {
			t.Errorf("%s: списки предпросмотра не должны быть nil", name)
		}
	}
	if _, err := e.svc.SaveNode(e.rev(t), "ffffffff", "", linkB); !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("неизвестная запись: %v", err)
	}
	if _, err := e.svc.PreviewNode(context.Background(), linkB, "ffffffff"); !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("предпросмотр неизвестной записи: %v", err)
	}
	// Имя проверяется как имя подписки: перевод строки и длина больше 128 символов.
	for label, name := range map[string]string{"перевод строки": "a\nproxies: []", "длинное": strings.Repeat("я", maxNameRunes+1)} {
		if _, err := e.svc.SaveNode(e.rev(t), id, name, linkB); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("имя «%s»: %v, нужна ErrInvalidInput", label, err)
		}
	}
	// Запись не тронута неудачными попытками.
	if got := e.my(t).Entries[0].Raw; got != linkA {
		t.Errorf("запись изменилась: %q", got)
	}
}

// Имя из ремарки ссылки при «Добавить» приводится к виду, который проходит
// проверку правки: без перевода строки и не длиннее 128 символов. Модаль
// «Изменить» отправляет сохранённое имя обратно, и правка сохраняется.
func TestRouterMyNodesAddSanitizesName(t *testing.T) {
	e := rtNewEnv(t, false, false)
	e.installed = rtBoth
	base := func(name string) string {
		link, _, _ := strings.Cut(rtLink(t, name), "#")
		return link
	}
	broken := base("vless_ws") + "#" + url.PathEscape("a\nproxies: []")
	long := base("trojan_ws") + "#" + url.PathEscape(strings.Repeat("я", maxNameRunes+40))
	e.addLinks(t, broken, long)

	entries := e.my(t).Entries
	if len(entries) != 2 {
		t.Fatalf("записей после «Добавить»: %d", len(entries))
	}
	for _, en := range entries {
		if strings.ContainsAny(en.Name, "\r\n") || utf8.RuneCountInString(en.Name) > maxNameRunes || en.Name == "" {
			t.Errorf("имя записи не приведено: %q", en.Name)
		}
		if _, err := e.svc.SaveNode(e.rev(t), en.ID, en.Name, en.Raw); err != nil {
			t.Errorf("правка с сохранённым именем %q: %v", en.Name, err)
		}
	}
	for _, name := range rtNames(rtMihomoProxies(t, rtGenerate(t, e.svc, kernelMihomo, e.draftInput(t), rtBoth))) {
		if strings.ContainsAny(name, "\r\n") {
			t.Errorf("перевод строки в имени узла Mihomo: %q", name)
		}
	}
}

func TestRouterMyNodesDeleteAndRaw(t *testing.T) {
	e := rtNewEnv(t, false, false)
	e.installed = rtBoth
	link := rtLink(t, "vless_reality")
	e.addLinks(t, link)
	id := e.my(t).Entries[0].ID

	raw, err := e.svc.NodeRaw(id)
	if err != nil {
		t.Fatal(err)
	}
	// Исходный текст отдаётся полностью, без маски (D-34).
	if raw.Raw != link || raw.EntryID != id || raw.Format == "" || strings.Contains(raw.Raw, "…") {
		t.Errorf("NodeRaw: %+v", raw)
	}
	if _, err := e.svc.NodeRaw("ffffffff"); !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("NodeRaw неизвестной записи: %v", err)
	}
	if _, err := e.svc.DeleteNode(e.rev(t), "ffffffff"); !errors.Is(err, ErrNodeNotFound) {
		t.Errorf("DeleteNode неизвестной записи: %v", err)
	}
	if _, err := e.svc.DeleteNode(e.rev(t)-1, id); !errors.Is(err, configlayer.ErrDraftConflict) {
		t.Errorf("DeleteNode на устаревшей ревизии: %v", err)
	}
	if _, err := e.svc.DeleteNode(e.rev(t), id); err != nil {
		t.Fatal(err)
	}
	st, _ := e.layer.ReadState()
	rawSection, ok := st.Draft[MySection]
	if !ok {
		t.Fatal("секция src_my исчезла после удаления последней записи")
	}
	cfg, err := parseMy(rawSection)
	if err != nil || len(cfg.Entries) != 0 {
		t.Errorf("секция после удаления: %+v, %v", cfg, err)
	}
	if files := rtGenerate(t, e.svc, kernelXray, e.draftInput(t), rtBoth); len(files) != 0 {
		t.Errorf("после удаления остался файл Xray")
	}
}

func TestRouterPreviewDupExists(t *testing.T) {
	e := rtNewEnv(t, false, false)
	e.installed = rtBoth
	ctx := context.Background()
	link := rtLink(t, "vless_ws")
	e.addLinks(t, link)

	view, err := e.svc.Preview(ctx, inputOf(link))
	if err != nil {
		t.Fatal(err)
	}
	if view.MyNodes == nil || len(view.MyNodes.Nodes) != 1 || view.Addable != 0 {
		t.Fatalf("повторная вставка: %+v", view)
	}
	if marks := view.MyNodes.Nodes[0].Marks; len(marks) != 1 || marks[0] != MarkDupExists {
		t.Errorf("метки: %v, нужна dup_exists", marks)
	}
	if view.Token != "" {
		t.Error("токен выдан, хотя добавлять нечего")
	}
	// Добавить то, что уже есть, нельзя даже по токену другой вставки.
	mixed, err := e.svc.Preview(ctx, inputOf(link+"\n"+rtLink(t, "vless_reality")))
	if err != nil || mixed.Addable != 1 || mixed.Token == "" {
		t.Fatalf("смешанная вставка: %v, %+v", err, mixed)
	}
	if res, err := e.svc.Add(e.rev(t), mixed.Token); err != nil || res.Nodes != 1 {
		t.Fatalf("Add: %v, %+v", err, res)
	}
	if n := len(e.my(t).Entries); n != 2 {
		t.Errorf("записей после Add: %d, нужно 2 (дубль не добавлен)", n)
	}

	// Одна ссылка трижды в одной вставке: один узел, схлопнуто два.
	other := rtLink(t, "trojan_grpc")
	triple, err := e.svc.Preview(ctx, inputOf(other+"\n"+other+"\n"+other))
	if err != nil {
		t.Fatal(err)
	}
	if triple.MyNodes == nil || len(triple.MyNodes.Nodes) != 1 || triple.Collapsed != 2 || triple.Addable != 1 {
		t.Errorf("тройная вставка: %+v", triple)
	}
}

func TestRouterPreviewNodeSingle(t *testing.T) {
	e := rtNewEnv(t, false, false)
	e.installed = rtBoth
	link := rtLink(t, "vless_ws")
	e.addLinks(t, link)
	id := e.my(t).Entries[0].ID

	// Правка самой записи: узел не помечается дублем самого себя.
	view, err := e.svc.PreviewNode(context.Background(), link, id)
	if err != nil {
		t.Fatal(err)
	}
	if view.MyNodes == nil || view.MyNodes.Count != 1 || len(view.MyNodes.Nodes[0].Marks) != 0 || view.Addable != 1 {
		t.Fatalf("предпросмотр правки: %+v", view)
	}
	n := view.MyNodes.Nodes[0]
	if !n.Support.Xray.OK || !n.Support.Mihomo.OK || n.CredMask == "" || strings.Contains(n.CredMask, rtUUIDA) {
		t.Errorf("узел предпросмотра: %+v", n)
	}
	// Тот же текст как новый узел — дубль.
	dup, err := e.svc.PreviewNode(context.Background(), link, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(dup.MyNodes.Nodes[0].Marks) != 1 || dup.MyNodes.Nodes[0].Marks[0] != MarkDupExists || dup.Addable != 0 {
		t.Errorf("новый узел с имеющимся текстом: %+v", dup)
	}
}
