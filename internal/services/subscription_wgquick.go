package services

import (
	"fmt"
	"net"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
)

// LooksLikeWgQuickConf распознаёт формат импорта для прежних обработчиков.
func LooksLikeWgQuickConf(content string) bool { return nodes.LooksLikeWgQuickConf(content) }

func looksLikeWgQuickConf(content string) bool { return nodes.LooksLikeWgQuickConf(content) }

func parseAWGField(awg *AWGOptions, key, value string) bool { return awg.Set(key, value) }

func normalizeAWGInitPacket(value string) string { return nodes.NormalizeAWGInitPacket(value) }

// parseAWGOptionsFromMap восстанавливает AWGOptions из универсальной карты параметров.
func parseAWGOptionsFromMap(m map[string]interface{}) *AWGOptions {
	if m == nil {
		return nil
	}
	opts := &AWGOptions{
		RawOptions: make(map[string]interface{}),
	}
	for k, v := range m {
		valStr := fmt.Sprintf("%v", v)
		if !opts.Set(k, valStr) {
			opts.RawOptions[k] = v
		}
	}
	if opts.IsEmpty() {
		return nil
	}
	return opts
}

func parseWgQuickConf(content string, tagPrefix string) ([]SubscriptionNode, error) {
	peers, err := nodes.ParseWgQuickPeers(content)
	if err != nil {
		return nil, err
	}
	out := make([]SubscriptionNode, 0, len(peers))
	for i, peer := range peers {
		tag := peer.Name
		if tag == "" {
			switch {
			case tagPrefix == "":
				host, _, _ := net.SplitHostPort(peer.Endpoint)
				tag = nodes.WgQuickPeerName(host, i, len(peers))
			case len(peers) == 1:
				tag = tagPrefix
			default:
				tag = fmt.Sprintf("%s-%d", tagPrefix, i+1)
			}
		}
		node := SubscriptionNode{
			Tag:            tag,
			Name:           peer.Name,
			Protocol:       "wireguard",
			Server:         peer.Endpoint,
			SecretKey:      peer.PrivateKey,
			PublicKey:      peer.PublicKey,
			PreSharedKey:   peer.PreSharedKey,
			LocalAddresses: peer.Address,
			DNS:            peer.DNS,
			MTU:            peer.MTU,
			AllowedIPs:     peer.AllowedIPs,
			KeepAlive:      peer.KeepAlive,
			AWG:            peer.AWG,
		}
		node.Dialect = string(DetectWireGuardDialect(&node))
		InferAWGVersion(&node)
		out = append(out, node)
	}
	return out, nil
}

// parseWgQuickConfToOutbounds разбирает конфигурацию wg-quick .conf в слайс Outbound для общего пайплайна.
func parseWgQuickConfToOutbounds(content string, sub *Subscription) ([]Outbound, []SkipReason, error) {
	tagPrefix := ""
	if sub != nil && sub.TagPrefix != "" {
		tagPrefix = sub.TagPrefix
	}
	nodes, err := parseWgQuickConf(content, tagPrefix)
	if err != nil {
		return nil, nil, err
	}
	outbounds := make([]Outbound, 0, len(nodes))
	for i := range nodes {
		ob, reason := wireguardNodeToOutbound(&nodes[i])
		if ob == nil {
			return nil, nil, fmt.Errorf("invalid wireguard peer #%d: %s", i+1, reason)
		}
		outbounds = append(outbounds, *ob)
	}
	return outbounds, nil, nil
}
