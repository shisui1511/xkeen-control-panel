package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Выбор узла подписки в ручном режиме Xray (SUBS-05).
//
// Xray собирает outbounds из всех файлов каталога конфигов в порядке имён
// файлов, а первый outbound итогового списка становится дефолтным. Панель
// пишет только свои файлы и управляет порядком их именами:
//
//   - 04_outbounds.<id>.tail.json — фрагмент подписки. Суффикс tail заставляет
//     Xray дописывать новые outbounds в конец, поэтому фрагмент не перехватывает
//     дефолт у файлов XKeen и пользователя (direct в шаблоне XKeen).
//   - 04_outbounds.zz_xcp_default.json — файл дефолта с одним выбранным узлом
//     под стабильным тегом xcp-<id>. Имя без tail и лексикографически позже
//     остальных, поэтому его outbound встаёт в начало итогового списка.
//   - tail-файл выбранных узлов (zz_xcp_selected) — выбранные узлы остальных подписок
//     под их стабильными тегами (в конце мерджа, дефолт не перехватывают).
const (
	selectionDefaultFileName = "04_outbounds.zz_xcp_default.json"
	// selectionTailFileName — файл выбранных узлов остальных подписок под их
	// стабильными тегами. Суффикс tail дописывает их в конец мерджа; имя
	// лексикографически после файла дефолта.
	selectionTailFileName = "04_outbounds.zz_xcp_selected.tail.json"
	stableTagPrefix       = "xcp-"
	// panelProxyTag — общий тег текущего дефолтного узла. Типовые шаблоны и
	// пресеты роутинга ссылаются на proxy; панель публикует его только если
	// outbound с таким тегом не объявлен в файлах XKeen или пользователя.
	panelProxyTag = "proxy"
)

var (
	// ErrStubNodeSelection — попытка выбрать узел-заглушку провайдера.
	ErrStubNodeSelection = errors.New("cannot select a provider stub node")
	// ErrSelectionNodeNotFound — выбранного узла нет среди узлов подписки.
	ErrSelectionNodeNotFound = errors.New("node not found in subscription outbounds")
	// ErrProtocolNotSupportedByXray — протокол узла (например, hysteria2/tuic)
	// не входит в allowedXrayProtocols: Xray не пишет для него outbound во
	// фрагмент, поэтому узел нельзя выбрать активным или использовать как цель
	// dialerProxy (WR-02 из код-ревью фазы 133).
	ErrProtocolNotSupportedByXray = errors.New("node protocol is not supported by Xray")

	safeIDRe = regexp.MustCompile(`^[a-z0-9_-]+$`)
)

// subscriptionSafeID возвращает ID подписки, пригодный для имени файла и тега:
// только [a-z0-9_-], иначе "safe_id".
func subscriptionSafeID(sub *Subscription) string {
	safeID := filepath.Base(sub.ID)
	safeID = invalidIDCharsRe.ReplaceAllString(strings.ToLower(safeID), "_")
	if !safeIDRe.MatchString(safeID) {
		safeID = "safe_id"
	}
	return safeID
}

// stableSubscriptionTag — стабильный тег выбранного узла подписки. Строится от
// неизменяемого ID (а не от provider_name, который пересчитывается при
// переименовании), поэтому правила роутинга пользователя со ссылкой на тег
// переживают и смену узла, и переименование подписки.
func stableSubscriptionTag(sub *Subscription) string {
	return stableTagPrefix + subscriptionSafeID(sub)
}

// selectionEligible — подписка вправе давать выбранный узел в конфиг Xray.
func selectionEligible(sub *Subscription) bool {
	return sub.Enabled && sub.EnableXray && sub.RoutingMode != "auto" && sub.SelectedTag != ""
}

// xraySelectable — узел пригоден для выбора активным/цели dialerProxy в
// Xray-подписке: не заглушка провайдера и протокол входит в
// allowedXrayProtocols. writeFragment пишет во фрагмент только такие узлы
// (subscription_converter.go), поэтому непригодный узел не найдётся ни по
// тегу во фрагменте (readFragmentOutboundLocked), ни в collectActiveXrayTags —
// выбор такого узла или каскад на него молча не применяется (WR-02).
func xraySelectable(n *SubscriptionNode) bool {
	return n != nil && !n.Stub && allowedXrayProtocols[n.Protocol]
}

// selectionTailPath — путь tail-файла выбранных узлов. Имя статическое.
func (s *SubscriptionService) selectionTailPath() string {
	return filepath.Join(s.configDir, selectionTailFileName)
}

// selectionDefaultPath — путь файла дефолта. Имя статическое, из ввода не выводится.
func (s *SubscriptionService) selectionDefaultPath() string {
	return filepath.Join(s.configDir, selectionDefaultFileName)
}

// cloneOutbound делает глубокую копию outbound через JSON, чтобы правка тега
// копии не затронула оригинал.
func cloneOutbound(ob map[string]interface{}) (map[string]interface{}, error) {
	raw, err := json.Marshal(ob)
	if err != nil {
		return nil, err
	}
	var cp map[string]interface{}
	if err := json.Unmarshal(raw, &cp); err != nil {
		return nil, err
	}
	return cp, nil
}

// foreignProxyOutboundFileLocked возвращает имя первого файла 04_outbounds*.json,
// который объявляет outbound с тегом proxy и не принадлежит панели (файлы выбора
// и фрагменты подписок исключаются). Пустая строка — тег proxy свободен и панель
// вправе его опубликовать. Нечитаемые и невалидные файлы пропускаются: они не
// объявляют outbound в мердже Xray. mu должен быть захвачен вызывающим (RLock
// достаточно).
func (s *SubscriptionService) foreignProxyOutboundFileLocked() string {
	if s.configDir == "" {
		return ""
	}
	entries, err := os.ReadDir(s.configDir)
	if err != nil {
		return ""
	}
	own := map[string]bool{
		s.selectionDefaultPath(): true,
		s.selectionTailPath():    true,
	}
	for i := range s.subscriptions {
		own[s.getFragmentPath(&s.subscriptions[i])] = true
		if legacy := s.legacyFragmentPath(&s.subscriptions[i]); legacy != "" {
			own[legacy] = true
		}
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "04_outbounds") || !strings.HasSuffix(name, ".json") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(s.configDir, name)
		if own[path] {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var wrapper struct {
			Outbounds []struct {
				Tag string `json:"tag"`
			} `json:"outbounds"`
		}
		if json.Unmarshal(data, &wrapper) != nil {
			continue
		}
		for _, ob := range wrapper.Outbounds {
			if ob.Tag == panelProxyTag {
				return name
			}
		}
	}
	return ""
}

// readFragmentOutboundLocked читает outbound с тегом nodeTag из фрагмента
// подписки как map, чтобы сохранить все поля, включая неизвестные структуре
// Outbound. Возвращает глубокую копию. mu должен быть захвачен вызывающим.
func (s *SubscriptionService) readFragmentOutboundLocked(sub *Subscription, nodeTag string) (map[string]interface{}, error) {
	data, err := os.ReadFile(s.getFragmentPath(sub))
	if err != nil {
		return nil, fmt.Errorf("outbounds file not found: %w", err)
	}
	var wrapper struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parse outbounds: %w", err)
	}
	for _, ob := range wrapper.Outbounds {
		if tag, _ := ob["tag"].(string); tag == nodeTag {
			raw, err := json.Marshal(ob)
			if err != nil {
				return nil, err
			}
			var cp map[string]interface{}
			if err := json.Unmarshal(raw, &cp); err != nil {
				return nil, err
			}
			return cp, nil
		}
	}
	return nil, fmt.Errorf("node %q: %w", nodeTag, ErrSelectionNodeNotFound)
}

// buildSelectionOutboundsLocked строит содержимое двух файлов выбора.
// defaultObs — копия узла дефолтной подписки под стабильным тегом (файл
// дефолта, первый в мердже). tailObs — копии выбранных узлов остальных
// подписок под их стабильными тегами в порядке списка подписок (tail-файл,
// в конце мерджа): они доступны только по тегу и дефолт не перехватывают.
// Стабильный тег встречается ровно в одном списке. Пусто в обоих — дефолтом
// остаётся первый outbound файлов XKeen.
//
// Общий тег proxy (D-03) публикуется, только если он не занят чужим outbound:
// при дефолте — вторым элементом defaultObs, копией дефолтного узла; без
// дефолта, но с выбором по тегу — заглушкой freedom в tailObs, чтобы правила со
// ссылкой на proxy оставались валидными и вели в direct. mu должен быть
// захвачен вызывающим.
func (s *SubscriptionService) buildSelectionOutboundsLocked() (defaultObs, tailObs []map[string]interface{}, err error) {
	for i := range s.subscriptions {
		sub := &s.subscriptions[i]
		if !selectionEligible(sub) {
			continue
		}
		ob, readErr := s.readFragmentOutboundLocked(sub, sub.SelectedTag)
		if sub.IsDefault {
			if readErr != nil {
				return nil, nil, readErr
			}
			ob["tag"] = stableSubscriptionTag(sub)
			defaultObs = append(defaultObs, ob)
			continue
		}
		if readErr != nil {
			// Выбор недефолтной подписки не должен ломать чужой выбор: узел
			// пропускается, фрагмент подписки восстановит следующий refresh.
			log.Printf("[Subscriptions] selected node of %s is unavailable for tag %s: %v",
				utils.SanitizeLogInput(sub.ID), stableSubscriptionTag(sub), readErr)
			continue
		}
		ob["tag"] = stableSubscriptionTag(sub)
		tailObs = append(tailObs, ob)
	}

	if s.foreignProxyOutboundFileLocked() == "" {
		switch {
		case len(defaultObs) > 0:
			proxyOb, cloneErr := cloneOutbound(defaultObs[0])
			if cloneErr != nil {
				return nil, nil, cloneErr
			}
			proxyOb["tag"] = panelProxyTag
			defaultObs = append(defaultObs, proxyOb)
		case len(tailObs) > 0:
			tailObs = append([]map[string]interface{}{{
				"tag":      panelProxyTag,
				"protocol": "freedom",
				"settings": map[string]interface{}{},
			}}, tailObs...)
		}
	}
	return defaultObs, tailObs, nil
}

// selectionFileWrite — планируемое изменение одного файла выбора.
type selectionFileWrite struct {
	path    string
	newData []byte // nil — файл должен отсутствовать
	oldData []byte
	existed bool
	changed bool
}

// planSelectionFile сравнивает желаемое содержимое файла с диском.
func planSelectionFile(path string, outbounds []map[string]interface{}) (selectionFileWrite, error) {
	w := selectionFileWrite{path: path}
	old, readErr := os.ReadFile(path)
	w.existed = readErr == nil
	w.oldData = old
	if len(outbounds) > 0 {
		data, err := json.MarshalIndent(map[string]interface{}{"outbounds": outbounds}, "", "  ")
		if err != nil {
			return w, err
		}
		w.newData = data
		w.changed = !w.existed || !bytes.Equal(old, data)
	} else {
		w.changed = w.existed
	}
	return w, nil
}

// apply записывает (или удаляет) файл.
func (w selectionFileWrite) apply() error {
	if w.newData == nil {
		if err := os.Remove(w.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return utils.AtomicWriteFile(w.path, w.newData, 0600)
}

// rollback возвращает файл к прежним байтам.
func (w selectionFileWrite) rollback() {
	if w.existed {
		_ = utils.AtomicWriteFile(w.path, w.oldData, 0600)
	} else {
		_ = os.Remove(w.path)
	}
}

// writeSelectionFilesLocked приводит файлы выбора (дефолт и tail) в
// соответствие с состоянием подписок: пишет их атомарно, удаляет пустые и
// откатывает оба к прежним байтам, если Xray отверг конфиг. Валидация одна на
// оба файла. Возвращает, изменился ли хотя бы один (нужен ли рестарт ядра).
// mu должен быть захвачен вызывающим.
func (s *SubscriptionService) writeSelectionFilesLocked() (bool, error) {
	if s.configDir == "" {
		return false, nil
	}
	defaultObs, tailObs, err := s.buildSelectionOutboundsLocked()
	if err != nil {
		return false, err
	}

	defaultPlan, err := planSelectionFile(s.selectionDefaultPath(), defaultObs)
	if err != nil {
		return false, err
	}
	tailPlan, err := planSelectionFile(s.selectionTailPath(), tailObs)
	if err != nil {
		return false, err
	}
	plans := []selectionFileWrite{defaultPlan, tailPlan}

	var changed []selectionFileWrite
	for _, p := range plans {
		if p.changed {
			changed = append(changed, p)
		}
	}
	if len(changed) == 0 {
		return false, nil
	}
	if defaultPlan.newData != nil || tailPlan.newData != nil {
		if err := os.MkdirAll(s.configDir, 0755); err != nil {
			return false, err
		}
	}
	for i, p := range changed {
		if err := p.apply(); err != nil {
			for _, done := range changed[:i] {
				done.rollback()
			}
			return false, err
		}
	}
	if ok, out := ValidateXrayConfigDir(s.configDir); !ok {
		for _, p := range changed {
			p.rollback()
		}
		return false, fmt.Errorf("Xray selection validation failed, rolled back: %s", out)
	}
	return true, nil
}
