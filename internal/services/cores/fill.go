package cores

import (
	"encoding/json"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Fill заполняет секции профиля панели Mihomo из шаблона или профиля пользователя
// (D-18 фазы 147): тот же перенос, что при взятии профиля, значения уходят в
// черновик, маршрутизация (proxies, proxy-groups, rules, providers) и закреплённые
// ключи не переносятся. Применяется обычным «Применить». Работает, только пока
// панель управляет Mihomo (иначе section_not_managed); data — текст шаблона или
// профиля, from — откуда взято (пустое имя — источник в секции не записывается:
// значения становятся панельными сразу, сверка по md5 при применении не нужна).
// Группа force-proxy из прежних секций сохраняется.
func (s *Service) Fill(rev int64, data []byte, from configlayer.SourceFile) (configlayer.DraftEvent, error) {
	if !s.d.Installed().Has(configlayer.KernelMihomo) {
		return configlayer.DraftEvent{}, errCode(codeKernelNotInstalled)
	}
	return s.edit(rev, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		if !configlayer.DesiredRouting(st.Draft).ManagedFor(configlayer.KernelMihomo) {
			return nil, errCode(codeSectionNotManaged)
		}
		values, err := SeedMihomo(data, configlayer.MihomoPorts{})
		if err != nil {
			return nil, errCode(configlayer.IssueCoreSectionInvalid)
		}
		prev := configlayer.DesiredCores(st.Draft)
		out := make(map[string]json.RawMessage, len(configlayer.MihomoSectionIDs))
		for _, id := range configlayer.MihomoSectionIDs {
			name := configlayer.CoreSectionName(configlayer.KernelMihomo, id)
			cs := configlayer.CoreSection{
				Managed:         true,
				Value:           values[id],
				ForceProxyGroup: prev[name].ForceProxyGroup,
			}
			if from.Name != "" {
				cs.From = []configlayer.SourceFile{from}
			}
			raw, err := encodeSection(cs)
			if err != nil {
				return nil, err
			}
			out[name] = raw
		}
		return out, nil
	})
}
