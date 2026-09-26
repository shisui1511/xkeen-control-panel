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
)

var (
	// ErrStubNodeSelection — попытка выбрать узел-заглушку провайдера.
	ErrStubNodeSelection = errors.New("cannot select a provider stub node")
	// ErrSelectionNodeNotFound — выбранного узла нет среди узлов подписки.
	ErrSelectionNodeNotFound = errors.New("node not found in subscription outbounds")

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

// selectionTailPath — путь tail-файла выбранных узлов. Имя статическое.
func (s *SubscriptionService) selectionTailPath() string {
	return filepath.Join(s.configDir, selectionTailFileName)
}

// selectionDefaultPath — путь файла дефолта. Имя статическое, из ввода не выводится.
func (s *SubscriptionService) selectionDefaultPath() string {
	return filepath.Join(s.configDir, selectionDefaultFileName)
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
// остаётся первый outbound файлов XKeen. mu должен быть захвачен вызывающим.
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
