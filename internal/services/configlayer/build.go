package configlayer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
)

// Коды проблем сборки.
const (
	IssueInvalidPanelName      = "invalid_panel_name"
	IssueStoplistName          = "stoplist_name"
	IssueTransportKeyForbidden = "transport_key_forbidden"
	IssueInvalidJSON           = "invalid_json"
	IssueNotObject             = "not_object"
	IssueProviderYAMLInvalid   = "provider_yaml_invalid"
	IssueProviderMissingKey    = "provider_missing_key"
	IssueProviderEmpty         = "provider_empty"
	IssueDuplicateFile         = "duplicate_file"
	IssueNameTaken             = "name_taken"
	IssueGeneratorFailed       = "generator_failed"
)

// BuildIssue — проблема одного файла сборки.
type BuildIssue struct {
	Key    string `json:"key"`
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// BuildError — все проблемы сборки разом.
type BuildError struct {
	Issues []BuildIssue
}

// Error перечисляет проблемы одной строкой.
func (e *BuildError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, is := range e.Issues {
		parts = append(parts, is.Key+": "+is.Code)
	}
	return "сборка файлов отклонена: " + strings.Join(parts, "; ")
}

// CheckGenerated проверяет сгенерированные файлы до записи и собирает все
// проблемы в один BuildError (пользователь видит их сразу). Проверки: имя по
// правилу панели и стоп-списку XKeen, имя не занято старым слоем
// (foreignOwned, может быть nil), для Xray — ключ "transport": (XKeen при нём
// выводит файл из работы), валидный JSON и объект на верхнем уровне, для
// провайдеров Mihomo — YAML с обязательным ключом и непустым списком. Файл
// не переименовывается и другое имя не подбирается.
func CheckGenerated(files []GeneratedFile, foreignOwned func(kernel, rel string) bool) error {
	var issues []BuildIssue
	add := func(f GeneratedFile, code, detail string) {
		issues = append(issues, BuildIssue{Key: f.Key(), Code: code, Detail: detail})
	}
	for _, f := range files {
		if err := ValidatePanelName(f.Kernel, f.RelPath); err != nil {
			code := IssueInvalidPanelName
			if errors.Is(err, ErrStoplistName) {
				code = IssueStoplistName
			}
			add(f, code, err.Error())
		}
		if foreignOwned != nil && foreignOwned(f.Kernel, f.RelPath) {
			add(f, IssueNameTaken, "имя занято файлом старого слоя")
		}
		switch f.Kind {
		case KindXrayJSON:
			for _, is := range checkXrayContent(f.Content) {
				add(f, is.Code, is.Detail)
			}
		case KindMihomoProxyProvider, KindMihomoRuleProvider:
			if code := ProviderContentProblem(f.Kind, f.Content); code != "" {
				add(f, code, "")
			}
		}
	}
	if len(issues) > 0 {
		return &BuildError{Issues: issues}
	}
	return nil
}

// checkXrayContent проверяет байты файла Xray: ключ "transport": ищется
// текстом (как у XKeen), затем JSON должен быть валидным объектом.
func checkXrayContent(content []byte) []BuildIssue {
	var out []BuildIssue
	if HasForbiddenTransport(content) {
		out = append(out, BuildIssue{Code: IssueTransportKeyForbidden, Detail: `ключ "transport": переименует файл в .obsolete`})
	}
	if !json.Valid(content) {
		return append(out, BuildIssue{Code: IssueInvalidJSON})
	}
	if trimmed := bytes.TrimSpace(content); len(trimmed) == 0 || trimmed[0] != '{' {
		out = append(out, BuildIssue{Code: IssueNotObject})
	}
	return out
}

// PlanAction — действие плана над файлом.
type PlanAction string

// Действия плана.
const (
	ActionWrite        PlanAction = "write"
	ActionDelete       PlanAction = "delete"
	ActionSkipReleased PlanAction = "skip_released"
)

// FilePlan — запланированное действие над одним файлом.
type FilePlan struct {
	Key            string
	Kernel         string
	RelPath        string
	AbsPath        string
	Action         PlanAction
	Kind           FileKind
	Content        []byte
	NewHash        string
	Expect         *Expectation
	RemoveObsolete bool
	WasReleased    bool
}

// Plan — результат расчёта: что писать, удалять, пропустить и какие сироты.
type Plan struct {
	Files   []FilePlan
	Orphans []OrphanFile
}

// Empty — в плане нет ни записи, ни удаления, ни сирот (skip_released
// действием не считается).
func (p Plan) Empty() bool {
	if len(p.Orphans) > 0 {
		return false
	}
	for _, fp := range p.Files {
		if fp.Action == ActionWrite || fp.Action == ActionDelete {
			return false
		}
	}
	return true
}

// Changes — у ядра есть действия плана: запись, удаление или сирота
// (сироты считаются изменением своего ядра). skip_released действием не является.
func (p Plan) Changes(kernel string) bool {
	for _, fp := range p.Files {
		if fp.Kernel == kernel && (fp.Action == ActionWrite || fp.Action == ActionDelete) {
			return true
		}
	}
	for _, o := range p.Orphans {
		if o.Kernel == kernel {
			return true
		}
	}
	return false
}

// ComputePlan рассчитывает план записи по желаемому набору файлов, манифесту и
// диску. Файл попадает в запись, если его нет на диске, хэш диска или манифеста
// отличается от нового, либо записи в манифесте нет; отпущенный файл (released)
// пропускается, пока его ключ не назван в only («Пересобрать»). Запись манифеста
// managed без генерации уходит в удаление, released без генерации не
// трогается. Если файла нет, а рядом лежит <имя>.obsolete, запись помечается
// RemoveObsolete. Сироты — файлы панели вне манифеста и вне генерации; при
// непустом only (пересборка названных файлов) сироты не собираются, а все
// остальные действия сужаются до ключей из only. Файлы отсортированы по ключу.
func ComputePlan(roots Roots, desired []GeneratedFile, manifest map[string]ManifestEntry, only map[string]bool, foreignOwned func(kernel, rel string) bool) (Plan, error) {
	restricted := len(only) > 0
	selected := func(key string) bool { return !restricted || only[key] }

	var plan Plan
	desiredKeys := make(map[string]bool, len(desired))
	for _, f := range desired {
		key := f.Key()
		desiredKeys[key] = true
		if !selected(key) {
			continue
		}
		abs, err := roots.Abs(f.Kernel, f.RelPath)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", key, err)
		}
		entry, inManifest := manifest[key]
		released := inManifest && entry.Status == StatusReleased
		fp := FilePlan{
			Key:         key,
			Kernel:      f.Kernel,
			RelPath:     f.RelPath,
			AbsPath:     abs,
			Kind:        f.Kind,
			Content:     f.Content,
			NewHash:     HashContent(f.Content),
			Expect:      f.Expect,
			WasReleased: released,
		}
		if released && !only[key] {
			fp.Action = ActionSkipReleased
			plan.Files = append(plan.Files, fp)
			continue
		}
		onDisk, err := os.ReadFile(abs)
		exists := err == nil
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return Plan{}, fmt.Errorf("%s: %w", key, err)
		}
		needWrite := !exists || HashContent(onDisk) != fp.NewHash ||
			!inManifest || entry.Hash != fp.NewHash || released
		if !needWrite {
			continue
		}
		fp.Action = ActionWrite
		if !exists {
			if _, statErr := os.Stat(abs + obsoleteSuffix); statErr == nil {
				fp.RemoveObsolete = true
			}
		}
		plan.Files = append(plan.Files, fp)
	}

	for key, entry := range manifest {
		if desiredKeys[key] || entry.Status != StatusManaged || !selected(key) {
			continue
		}
		abs, err := roots.Abs(entry.Kernel, entry.RelPath)
		if err != nil {
			return Plan{}, fmt.Errorf("%s: %w", key, err)
		}
		plan.Files = append(plan.Files, FilePlan{
			Key:     key,
			Kernel:  entry.Kernel,
			RelPath: entry.RelPath,
			AbsPath: abs,
			Action:  ActionDelete,
			Kind:    entry.Kind,
		})
	}
	sort.Slice(plan.Files, func(i, j int) bool { return plan.Files[i].Key < plan.Files[j].Key })

	if !restricted {
		orphans, err := ScanOrphans(roots, manifest, foreignOwned)
		if err != nil {
			return Plan{}, err
		}
		for _, o := range orphans {
			if !desiredKeys[o.Key] {
				plan.Orphans = append(plan.Orphans, o)
			}
		}
	}
	return plan, nil
}
