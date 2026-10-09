package nodes

import (
	"net"
	"strconv"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// wgQuickOnly — ключи [Interface] обычного wg-quick, которые к туннелю клиента
// не относятся. Старый слой складывает неизвестные ключи в RawOptions и по ним
// определяет AmneziaWG 3.1, поэтому до разбора они убираются из текста.
var wgQuickOnly = map[string]bool{
	"table": true, "preup": true, "postup": true, "predown": true, "postdown": true,
	"saveconfig": true, "listenport": true, "fwmark": true,
}

// wgBlock — один блок [Interface] с идущими за ним [Peer].
type wgBlock struct {
	line  int
	text  string // исходный текст блока
	clean string // текст для разбора без ключей wgQuickOnly
}

// wgBlocks делит текст на блоки по заголовкам [Interface].
func wgBlocks(text string) []wgBlock {
	var blocks []wgBlock
	var orig, clean []string
	start := 0
	section := ""
	flush := func() {
		if len(orig) > 0 {
			blocks = append(blocks, wgBlock{
				line:  start,
				text:  strings.TrimRight(strings.Join(orig, "\n"), "\n"),
				clean: strings.Join(clean, "\n"),
			})
		}
		orig, clean = nil, nil
	}
	for i, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if section == "interface" {
				flush()
				start = i + 1
			}
		}
		if len(orig) == 0 && section != "interface" {
			continue // текст до первого [Interface]
		}
		orig = append(orig, raw)
		if section == "interface" && !strings.HasPrefix(line, "[") {
			if k, _, ok := strings.Cut(line, "="); ok && wgQuickOnly[strings.ToLower(strings.TrimSpace(k))] {
				continue
			}
		}
		clean = append(clean, raw)
	}
	flush()
	return blocks
}

// wgMissing — ошибка отсутствия обязательного ключа.
func wgMissing(key, section string) *SourceError {
	return NewError(CodeWGMissingKey, "key", key, "section", section)
}

// wgPrecheck проверяет обязательные ключи блока: [Interface] PrivateKey и
// Address, у каждого [Peer] PublicKey и Endpoint (регистр ключей не важен).
func wgPrecheck(b wgBlock) *SourceError {
	section := ""
	var hasPriv, hasAddr bool
	type peer struct{ pub, endpoint bool }
	var peers []peer
	for _, raw := range strings.Split(b.clean, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if section == "peer" {
				peers = append(peers, peer{})
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		switch section {
		case "interface":
			switch key {
			case "privatekey", "secretkey":
				hasPriv = true
			case "address":
				hasAddr = true
			}
		case "peer":
			if len(peers) == 0 {
				continue
			}
			switch key {
			case "publickey":
				peers[len(peers)-1].pub = true
			case "endpoint":
				peers[len(peers)-1].endpoint = true
			}
		}
	}
	switch {
	case !hasPriv:
		return wgMissing("PrivateKey", "Interface")
	case !hasAddr:
		return wgMissing("Address", "Interface")
	case len(peers) == 0:
		return wgMissing("PublicKey", "Peer")
	}
	for _, p := range peers {
		if !p.pub {
			return wgMissing("PublicKey", "Peer")
		}
		if !p.endpoint {
			return wgMissing("Endpoint", "Peer")
		}
	}
	return nil
}

// ParseWG разбирает wg.conf (в том числе AWG 1.0/1.5/2.0/3.x) через разбор
// старого слоя. Несколько блоков [Interface] подряд дают несколько узлов.
// Блок без обязательного ключа — wg_missing_key с key и section: единственная
// ошибка возвращается как error, при нескольких блоках — как Skipped.
func ParseWG(text string) (ParseResult, error) {
	res := ParseResult{Format: "wg"}
	if len(text) > maxBodyBytes {
		return res, NewError(CodeTooLarge)
	}
	blocks := wgBlocks(text)
	if len(blocks) == 0 {
		return res, wgMissing("PrivateKey", "Interface")
	}
	var first *SourceError
	for _, b := range blocks {
		nodes, err := wgBlockNodes(b)
		if err != nil {
			if first == nil {
				first = err
			}
			res.Skipped = append(res.Skipped, skippedFrom(err, b.line, "", "wireguard"))
			continue
		}
		res.Nodes = append(res.Nodes, nodes...)
		if len(res.Nodes) > maxNodes {
			return res, NewError(CodeTooLarge)
		}
	}
	if len(res.Nodes) == 0 && first != nil {
		return res, first
	}
	return res, nil
}

func wgBlockNodes(b wgBlock) ([]Node, *SourceError) {
	if err := wgPrecheck(b); err != nil {
		return nil, err
	}
	subs, err := services.ParseWgQuickConf(b.clean)
	if err != nil {
		// Ключи есть, но не разбираются (не base64 на 32 байта и т. п.).
		return nil, NewError(CodeUnrecognized)
	}
	out := make([]Node, 0, len(subs))
	for i := range subs {
		sn := &subs[i]
		host, portStr, serr := net.SplitHostPort(sn.Server)
		port, perr := strconv.Atoi(portStr)
		if serr != nil || perr != nil || host == "" || port < 1 || port > 65535 {
			return nil, NewError(CodeUnrecognized)
		}
		wg := &WireGuard{
			PrivateKey:    sn.SecretKey,
			PeerPublicKey: sn.PublicKey,
			PreSharedKey:  sn.PreSharedKey,
			Address:       sn.LocalAddresses,
			MTU:           sn.MTU,
			AllowedIPs:    sn.AllowedIPs,
			Reserved:      sn.Reserved,
			AWGVersion:    services.InferAWGVersion(sn),
		}
		if sn.AWG != nil && !sn.AWG.IsEmpty() {
			wg.AWG = awgOptionsMap(sn.AWG)
		}
		out = append(out, Node{
			Name:     orDefault(sn.Name, sn.Tag),
			Protocol: "wireguard",
			Host:     host,
			Port:     port,
			WG:       wg,
			Origin:   OriginWGConf,
			Text:     b.text,
			Format:   "wg",
		})
	}
	return out, nil
}

// awgOptionsMap переводит опции AmneziaWG в карту с ключами Mihomo
// (amnezia-wg-option): только заданные поля.
func awgOptionsMap(o *services.AWGOptions) map[string]any {
	m := map[string]any{}
	ints := map[string]*int{
		"jc": o.Jc, "jmin": o.Jmin, "jmax": o.Jmax, "s1": o.S1, "s2": o.S2, "s3": o.S3, "s4": o.S4,
		"j1": o.J1, "j2": o.J2, "j3": o.J3, "itime": o.Itime,
		"content-padding-addition": o.ContentPaddingAddition, "rekey-after-time": o.RekeyAfterTime,
		"rekey-timeout": o.RekeyTimeout, "reject-after-time": o.RejectAfterTime,
		"keepalive-timeout": o.KeepaliveTimeout, "max-handshake-attempts": o.MaxHandshakeAttempts,
	}
	for k, p := range ints {
		if p != nil {
			m[k] = *p
		}
	}
	strs := map[string]string{
		"h1": o.H1, "h2": o.H2, "h3": o.H3, "h4": o.H4,
		"i1": o.I1, "i2": o.I2, "i3": o.I3, "i4": o.I4, "i5": o.I5,
		"version": o.Version, "header-protection-key": o.HeaderProtectionKey,
	}
	for k, v := range strs {
		if v != "" {
			m[k] = v
		}
	}
	if o.RandomTrailers != nil {
		m["random-trailers"] = *o.RandomTrailers
	}
	if o.DisableCookies != nil {
		m["disable-cookies"] = *o.DisableCookies
	}
	for k, v := range o.RawOptions {
		m[strings.ToLower(k)] = v
	}
	return m
}
