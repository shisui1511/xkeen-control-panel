package geodata

// OutputChunk — окно вывода задания (ug или jq) для клиента: поля по
// frontend/src/lib/geodataContract.json.
type OutputChunk struct {
	Job       string `json:"job"`
	From      int64  `json:"from"`
	Next      int64  `json:"next"`
	Text      string `json:"text"`
	Running   bool   `json:"running"`
	Truncated bool   `json:"truncated,omitempty"`
}
