package services

import "github.com/shisui1511/xkeen-control-panel/internal/nodes"

func DetectWireGuardDialect(node *SubscriptionNode) nodes.WireGuardDialect {
	if node == nil {
		return nodes.DialectPlain
	}
	return nodes.DetectWireGuardDialect(node.AWG)
}

func InferAWGVersion(node *SubscriptionNode) string {
	if node == nil {
		return ""
	}
	version := nodes.InferAWGVersion(node.AWG)
	if node.AWG != nil {
		node.AWG.Version = version
	}
	return version
}
