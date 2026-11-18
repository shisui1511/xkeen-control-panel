package nodes

import "strings"

// wgBlock — один блок [Interface] с идущими за ним [Peer].
type wgBlock struct {
	line int
	text string // исходный текст блока
}

// wgBlocks делит текст на блоки по заголовкам [Interface].
func wgBlocks(text string) []wgBlock {
	var blocks []wgBlock
	var orig []string
	start := 0
	section := ""
	flush := func() {
		if len(orig) > 0 {
			blocks = append(blocks, wgBlock{
				line: start,
				text: strings.TrimRight(strings.Join(orig, "\n"), "\n"),
			})
		}
		orig = nil
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
	for _, raw := range strings.Split(b.text, "\n") {
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

// ParseWG разбирает wg.conf (в том числе AWG 1.0/1.5/2.0/3.x) в узлы.
// Несколько блоков [Interface] подряд дают несколько узлов.
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
	out, err := ParseWgQuickConf(b.text)
	if err != nil {
		// Обязательные поля на месте, но значение одного из них не разбирается.
		return nil, NewError(CodeUnrecognized)
	}
	return out, nil
}
