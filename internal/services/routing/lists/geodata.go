package lists

// GeodataEntry — запись .xkeen.xray.geodata[] в xkeen.json: файл и адрес, откуда его
// скачивает XKeen.
type GeodataEntry struct {
	File string `json:"file"`
	URL  string `json:"url"`
}

// GeodataStatus — состояние пользовательских geodata XKeen.
type GeodataStatus struct {
	JQ            bool           `json:"jq"`
	GeofileUpdate bool           `json:"geofile_update"`
	Entries       []GeodataEntry `json:"entries"`
	Reason        string         `json:"reason,omitempty"`
}
