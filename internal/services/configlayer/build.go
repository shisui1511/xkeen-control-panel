package configlayer

import "strings"

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

// CheckGenerated проверяет сгенерированные файлы до записи.
func CheckGenerated(files []GeneratedFile, foreignOwned func(kernel, rel string) bool) error {
	return nil
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
	return false
}

// Changes — у ядра есть действия плана (сироты считаются изменением ядра).
func (p Plan) Changes(kernel string) bool {
	return false
}

// ComputePlan рассчитывает план записи.
func ComputePlan(roots Roots, desired []GeneratedFile, manifest map[string]ManifestEntry, only map[string]bool, foreignOwned func(kernel, rel string) bool) (Plan, error) {
	return Plan{}, nil
}
