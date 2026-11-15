package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// xrayHot меняет узел ручной группы без рестарта (143 Q5, 146 D-38).
// Сравнение итоговых тел охватывает также внешние вложенные группы (146 D-08).
func (s *Service) xrayHot(ctx context.Context, files []configlayer.HotFile) error {
	return s.xrayHotApply(ctx, files, false)
}

func (s *Service) xrayHotConfirm(ctx context.Context, files []configlayer.HotFile) error {
	return s.xrayHotApply(ctx, files, true)
}

func (s *Service) xrayHotApply(ctx context.Context, files []configlayer.HotFile, confirm bool) error {
	fail := func(reason string, partial bool) error {
		return &configlayer.XrayHotError{Reason: reason, Partial: partial}
	}
	if s.d.XrayBin == nil || s.d.XrayBin() == "" {
		return fail(configlayer.HotReasonNoAPI, false)
	}
	bin := s.d.XrayBin()
	if info, err := os.Stat(bin); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return fail(configlayer.HotReasonNoAPI, false)
	}
	version := s.d.KernelVersion(configlayer.KernelXray)
	if configlayer.FeatureAvailable(configlayer.KernelXray, version, configlayer.FeatureXrayHotSwapAPI) == configlayer.Unavailable {
		return fail(configlayer.HotReasonVersion, false)
	}
	st, err := s.d.Layer.ReadState()
	if err != nil || st.Routing.XrayForeignAPI {
		return fail(configlayer.HotReasonNoAPI, false)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.xrayAPIPort()))
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return fail(configlayer.HotReasonNoAPI, false)
	}
	_ = conn.Close()
	if len(files) != 1 || files[0].RelPath != xrayGroupsFile || files[0].Action != configlayer.ActionWrite || !files[0].OldExisted {
		return fail(configlayer.HotReasonNotApplicable, false)
	}
	tags, next, prev, err := hotTags(files[0].Old, files[0].New)
	if err != nil {
		return fail(configlayer.HotReasonNotApplicable, false)
	}
	if confirm {
		// После прерывания тело на диске не доказывает состояние процесса.
		pending := readUnconfirmed(st.Live)
		seen := make(map[string]bool, len(tags))
		for _, tag := range tags {
			seen[tag] = true
		}
		for tag := range next {
			if len(tag) == len(GroupTag("12345678")) && pending[tag[len(groupTagPrefix):]] == configlayer.KernelXray && !seen[tag] {
				tags = append(tags, tag)
			}
		}
		sort.Strings(tags)
	}
	started := time.Now()
	defer func() {
		log.Printf("[routing] горячая замена Xray: тегов %d, rmo→ado %s", len(tags), time.Since(started))
	}()
	for i, tag := range tags {
		if _, err := hotCommand(ctx, bin, addr, "rmo", tag); err != nil {
			return fail(configlayer.HotReasonFailed, i > 0)
		}
		if err := hotAdd(ctx, bin, addr, next[tag]); err != nil {
			// Одна попытка вернуть удалённый тег; новое намерение доведёт рестарт.
			_ = hotAdd(ctx, bin, addr, prev[tag])
			return fail(configlayer.HotReasonFailed, true)
		}
	}
	if len(tags) > 0 && configlayer.FeatureAvailable(configlayer.KernelXray, version, configlayer.FeatureXrayListOutbounds) == configlayer.Available {
		out, err := hotCommand(ctx, bin, addr, "lso")
		if err != nil {
			return fail(configlayer.HotReasonFailed, true)
		}
		// lso возвращает JSON: проверяем точные строки тегов, без совпадения префиксов.
		for _, tag := range tags {
			quoted, _ := json.Marshal(tag)
			if !bytes.Contains(out, quoted) {
				return fail(configlayer.HotReasonFailed, true)
			}
		}
	}
	return nil
}

// hotTags принимает только сгенерированные ручные исходящие панели.
func hotTags(old, next []byte) (changed []string, newOut, oldOut map[string]json.RawMessage, err error) {
	parse := func(data []byte) (map[string]json.RawMessage, error) {
		var doc struct {
			Outbounds []json.RawMessage `json:"outbounds"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
		out := make(map[string]json.RawMessage, len(doc.Outbounds))
		for _, raw := range doc.Outbounds {
			var body map[string]any
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			if err := decoder.Decode(&body); err != nil {
				return nil, err
			}
			tag, ok := body["tag"].(string)
			if !ok {
				return nil, errors.New("invalid group tag")
			}
			if tag != TagDirect && tag != TagBlock {
				if len(tag) != len(GroupTag("12345678")) {
					return nil, errors.New("invalid group tag")
				}
				id := tag[len(tag)-8:]
				if tag != GroupTag(id) || !idRE.MatchString(id) {
					return nil, errors.New("invalid group tag")
				}
			}
			if _, exists := out[tag]; exists {
				return nil, errors.New("duplicate group tag")
			}
			canonical, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			out[tag] = canonical
		}
		return out, nil
	}
	if oldOut, err = parse(old); err != nil {
		return
	}
	if newOut, err = parse(next); err != nil {
		return
	}
	if len(oldOut) != len(newOut) {
		err = errors.New("changed group tags")
		return
	}
	for tag, body := range newOut {
		before, ok := oldOut[tag]
		if !ok {
			err = errors.New("changed group tags")
			return
		}
		if !bytes.Equal(before, body) {
			if tag == TagDirect || tag == TagBlock {
				err = errors.New("changed service outbound")
				return
			}
			changed = append(changed, tag)
		}
	}
	sort.Strings(changed)
	return
}

func hotCommand(ctx context.Context, bin, addr, op string, tail ...string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := append([]string{"api", op, "-s", addr, "-t", "3"}, tail...)
	cmd := exec.CommandContext(cctx, bin, args...)
	cmd.Env = utils.XrayAssetEnv(os.Environ(), "")
	out, err := cmd.Output()
	code := 0
	if err != nil {
		code = -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
	}
	log.Printf("[routing] Xray api %s: код %d", op, code)
	return out, err
}

func hotAdd(ctx context.Context, bin, addr string, body json.RawMessage) error {
	dir, err := os.MkdirTemp("", "xcp-hot-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	data, err := json.Marshal(struct {
		Outbounds []json.RawMessage `json:"outbounds"`
	}{[]json.RawMessage{body}})
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "outbound.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	_, err = hotCommand(ctx, bin, addr, "ado", path)
	if err != nil {
		return fmt.Errorf("ado: %w", err)
	}
	return nil
}
