package configlayer

import (
	"bytes"
	"encoding/json"
	"errors"
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

// Empty — в плане нет ни записи, ни удаления, ни сирот.
func (p Plan) Empty() bool {
	return len(p.Files) == 0 && len(p.Orphans) == 0
}

// Changes — у ядра есть действия плана (сироты считаются изменением ядра).
func (p Plan) Changes(kernel string) bool {
	return false
}

// ComputePlan рассчитывает план записи.
func ComputePlan(roots Roots, desired []GeneratedFile, manifest map[string]ManifestEntry, only map[string]bool, foreignOwned func(kernel, rel string) bool) (Plan, error) {
	var plan Plan
	for _, f := range desired {
		abs, err := roots.Abs(f.Kernel, f.RelPath)
		if err != nil {
			return Plan{}, err
		}
		plan.Files = append(plan.Files, FilePlan{
			Key:     f.Key(),
			Kernel:  f.Kernel,
			RelPath: f.RelPath,
			AbsPath: abs,
			Action:  ActionWrite,
			Kind:    f.Kind,
			Content: f.Content,
			NewHash: HashContent(f.Content),
			Expect:  f.Expect,
		})
	}
	return plan, nil
}
