package routing

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Переход панель ↔ свой профиль Mihomo одной операцией (D-06, D-07, D-09, D-10):
// правка секции routing и секций Mihomo в черновик и конвейер «Применить» идут под
// одним замком применения (Layer.ApplyEdit). Затрагивается только Mihomo: решение по
// Xray, его секции и файлы не меняются. Значения секций Mihomo при отпускании
// сохраняются и возвращаются при возврате.

// ErrProfileUnknown — профиля с таким именем нет среди своих профилей Mihomo: имя
// недопустимо, это профиль панели (xcp-) или файла profiles/<имя>.yaml нет.
var ErrProfileUnknown = errors.New("routing: профиль Mihomo не найден")

// ActivateProfile делает профиль name активным вместо профиля панели: Mihomo
// отпускается на свой профиль name (D-07, D-09), значения секций Mihomo сохраняются.
// Применение синхронное; при отказе черновик возвращается, панель остаётся как была.
func (s *Service) ActivateProfile(ctx context.Context, name string) (configlayer.ApplyView, error) {
	if !s.d.Layer.MihomoProfileUsable(name) {
		return configlayer.ApplyView{}, ErrProfileUnknown
	}
	return s.releaseMihomo(ctx, name)
}

// ReleaseMihomo отпускает Mihomo на прежний профиль (тот, что был до взятия), Xray не
// трогается.
func (s *Service) ReleaseMihomo(ctx context.Context) (configlayer.ApplyView, error) {
	return s.releaseMihomo(ctx, "")
}

// releaseMihomo — общий ход активации и отпускания: секция routing с решением
// «Mihomo на своём профиле» (profile пуст — на прежний) и секции Mihomo без
// управления, но со значениями.
func (s *Service) releaseMihomo(ctx context.Context, profile string) (configlayer.ApplyView, error) {
	return s.d.Layer.ApplyEdit(ctx, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		if !s.d.Layer.MihomoTaken() {
			return nil, &InvalidInputError{Field: "action", Reason: "action_invalid"}
		}
		next := configlayer.DesiredRouting(st.Draft).WithMihomo(false, profile)
		next.V = sectionVersion
		raw, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		sections := map[string]json.RawMessage{configlayer.RoutingSection: raw}
		released, err := releaseMihomoSections(st.Draft)
		if err != nil {
			return nil, err
		}
		for name, v := range released {
			sections[name] = v
		}
		return sections, nil
	})
}

// ReturnMihomo возвращает Mihomo панели: решение «Mihomo под панелью» (Xray не
// меняется), «Остальное → DIRECT» при пустых правилах, секции Mihomo — сохранённые
// значения или перенос из активного профиля (как при взятии области mihomo).
func (s *Service) ReturnMihomo(ctx context.Context) (configlayer.ApplyView, error) {
	return s.d.Layer.ApplyEdit(ctx, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		if s.d.Layer.MihomoTaken() {
			return nil, &InvalidInputError{Field: "action", Reason: "action_invalid"}
		}
		next := desiredAfterTake(configlayer.DesiredRouting(st.Draft), ScopeMihomo)
		return s.takeSections(st, ScopeMihomo, next)
	})
}
