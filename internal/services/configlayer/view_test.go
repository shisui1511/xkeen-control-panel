package configlayer

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSnapshotView_JSONKeys(t *testing.T) {
	data, err := json.Marshal(SnapshotView{Files: []FileView{}, Notices: []NoticeView{}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"enabled", "dev_mode", "draft_revision", "draft_changes", "drift_count",
		"files", "kernels", "features", "apply", "notices",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("в JSON снимка нет ключа %q: %s", key, data)
		}
	}
}

func TestSortFileViews(t *testing.T) {
	files := []FileView{
		{Key: "k5", Path: "/a/5", State: StateReleased},
		{Key: "k3", Path: "/a/3", State: StateOK},
		{Key: "k2", Path: "/a/2", State: StatePending},
		{Key: "k4", Path: "/a/0", State: StateOK},
		{Key: "k1b", Path: "/a/9", State: StateDriftRenamed},
		{Key: "k1a", Path: "/a/1", State: StateDriftModified},
	}
	sortFileViews(files)
	var got []string
	for _, f := range files {
		got = append(got, f.Key)
	}
	want := []string{"k1a", "k1b", "k2", "k4", "k3", "k5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("порядок = %v, want %v (drift, pending, ok, released; внутри по пути)", got, want)
	}
}
