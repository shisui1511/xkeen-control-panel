package sources

import (
	"os"
	"sort"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// ProfileSource — источник, чей файл провайдера Mihomo попадает в профиль из
// шаблона: идентификатор и число узлов Mihomo в нём.
type ProfileSource struct {
	ID    string
	Nodes int
}

// MihomoProfileSources — источники для нового профиля из шаблона (D-02): включённые,
// с узлами Mihomo в применённом состоянии, файл провайдера которых есть на диске.
// Новый, ещё не применённый источник сюда не входит, как и файл, который слой не
// записал (ссылка на него дала бы пустой провайдер). «Мои узлы» идут первыми,
// остальные по идентификатору.
func (s *Service) MihomoProfileSources() ([]ProfileSource, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return nil, err
	}
	installed := s.d.Installed()
	cat, err := s.Catalog(configlayer.Input{Sections: st.Applied, Live: st.Live, Mode: configlayer.SourceApplied}, installed)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, n := range cat.Mihomo {
		counts[n.Source]++
	}
	if len(counts) == 0 {
		return []ProfileSource{}, nil
	}
	files := s.d.Layer.Snapshot().Files
	out := make([]ProfileSource, 0, len(counts))
	for id, n := range counts {
		if profileFileOnDisk(files, id) {
			out = append(out, ProfileSource{ID: id, Nodes: n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].ID == MySourceID) != (out[j].ID == MySourceID) {
			return out[i].ID == MySourceID
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// profileFileOnDisk — файл провайдера источника записан слоем: запись Mihomo с
// суффиксом /proxy_providers/xcp-<id>.yaml, не потерянная (drift_missing) и
// существующая на диске.
func profileFileOnDisk(files []configlayer.FileView, id string) bool {
	suffix := "/proxy_providers/xcp-" + id + ".yaml"
	for _, f := range files {
		if f.Kernel != configlayer.KernelMihomo || !strings.HasSuffix(f.Path, suffix) {
			continue
		}
		if f.State == configlayer.StateDriftMissing {
			return false
		}
		_, err := os.Stat(f.Path)
		return err == nil
	}
	return false
}
