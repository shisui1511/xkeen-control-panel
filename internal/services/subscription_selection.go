package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
const (
	selectionDefaultFileName = "04_outbounds.zz_xcp_default.json"
	stableTagPrefix          = "xcp-"
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

// buildSelectionOutboundsLocked строит содержимое файла дефолта: копию узла
// дефолтной подписки под стабильным тегом. Пусто — дефолтом остаётся первый
// outbound файлов XKeen. mu должен быть захвачен вызывающим.
func (s *SubscriptionService) buildSelectionOutboundsLocked() ([]map[string]interface{}, error) {
	for i := range s.subscriptions {
		sub := &s.subscriptions[i]
		if !sub.IsDefault || !selectionEligible(sub) {
			continue
		}
		ob, err := s.readFragmentOutboundLocked(sub, sub.SelectedTag)
		if err != nil {
			return nil, err
		}
		ob["tag"] = stableSubscriptionTag(sub)
		return []map[string]interface{}{ob}, nil
	}
	return nil, nil
}

// writeSelectionFilesLocked приводит файл дефолта в соответствие с состоянием
// подписок: пишет его атомарно, удаляет, когда дефолтного узла нет, и
// откатывает прежние байты, если Xray отверг конфиг. Возвращает, изменился ли
// файл (нужен ли рестарт ядра). mu должен быть захвачен вызывающим.
func (s *SubscriptionService) writeSelectionFilesLocked() (bool, error) {
	if s.configDir == "" {
		return false, nil
	}
	outbounds, err := s.buildSelectionOutboundsLocked()
	if err != nil {
		return false, err
	}

	path := s.selectionDefaultPath()
	oldData, readErr := os.ReadFile(path)
	existed := readErr == nil

	if len(outbounds) == 0 {
		if !existed {
			return false, nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, err
		}
		if ok, out := ValidateXrayConfigDir(s.configDir); !ok {
			_ = utils.AtomicWriteFile(path, oldData, 0600)
			return false, fmt.Errorf("Xray validation failed after clearing default node, rolled back: %s", out)
		}
		return true, nil
	}

	newData, err := json.MarshalIndent(map[string]interface{}{"outbounds": outbounds}, "", "  ")
	if err != nil {
		return false, err
	}
	if existed && bytes.Equal(oldData, newData) {
		return false, nil
	}
	if err := os.MkdirAll(s.configDir, 0755); err != nil {
		return false, err
	}
	if err := utils.AtomicWriteFile(path, newData, 0600); err != nil {
		return false, err
	}
	if ok, out := ValidateXrayConfigDir(s.configDir); !ok {
		if existed {
			_ = utils.AtomicWriteFile(path, oldData, 0600)
		} else {
			_ = os.Remove(path)
		}
		return false, fmt.Errorf("Xray default node validation failed, rolled back: %s", out)
	}
	return true, nil
}
