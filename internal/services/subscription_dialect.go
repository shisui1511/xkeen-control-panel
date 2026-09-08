package services

import "strings"

// WireGuardDialect представляет тип/диалект конфигурации WireGuard/AmneziaWG.
type WireGuardDialect string

const (
	DialectPlain   WireGuardDialect = "plain"
	DialectClassic WireGuardDialect = "classic"
	Dialect15      WireGuardDialect = "1.5"
	Dialect20      WireGuardDialect = "2.0"
	Dialect31      WireGuardDialect = "3.1"
)

// DetectWireGuardDialect определяет диалект WireGuard/AmneziaWG узла:
// - "plain": чистый WireGuard без параметров обфускации;
// - "classic": AmneziaWG 1.0 (Jc, Jmin, Jmax, S1, S2, H1-H4);
// - "1.5": AmneziaWG 1.5 (J1, J2, J3, Itime);
// - "2.0": AmneziaWG 2.0 (наличие S3 или S4);
// - "3.1": AmneziaWG 3.1 (Version, HeaderProtectionKey, I1-I5, ContentPaddingAddition, RandomTrailers, DisableCookies, RekeyAfterTime, RekeyTimeout, RejectAfterTime, KeepaliveTimeout, MaxHandshakeAttempts или RawOptions).
func DetectWireGuardDialect(node *SubscriptionNode) WireGuardDialect {
	if node == nil || node.AWG == nil || node.AWG.IsEmpty() {
		return DialectPlain
	}

	awg := node.AWG

	// Явная версия в AWGOptions (если указана)
	ver := strings.ToLower(strings.TrimSpace(awg.Version))
	if strings.HasPrefix(ver, "3") || strings.HasPrefix(ver, "v3") {
		return Dialect31
	}
	if strings.HasPrefix(ver, "2") || strings.HasPrefix(ver, "v2") {
		return Dialect20
	}
	if strings.HasPrefix(ver, "1.5") || strings.HasPrefix(ver, "v1.5") {
		return Dialect15
	}
	if strings.HasPrefix(ver, "1") || strings.HasPrefix(ver, "v1") {
		return DialectClassic
	}

	// 1. Проверка параметров диалекта 3.1 (наивысший приоритет)
	if awg.Version != "" ||
		awg.HeaderProtectionKey != "" ||
		awg.I1 != "" || awg.I2 != "" || awg.I3 != "" || awg.I4 != "" || awg.I5 != "" ||
		awg.ContentPaddingAddition != nil ||
		awg.RandomTrailers != nil ||
		awg.DisableCookies != nil ||
		awg.RekeyAfterTime != nil ||
		awg.RekeyTimeout != nil ||
		awg.RejectAfterTime != nil ||
		awg.KeepaliveTimeout != nil ||
		awg.MaxHandshakeAttempts != nil ||
		len(awg.RawOptions) > 0 {
		return Dialect31
	}

	// 2. Проверка параметров диалекта 2.0 (S3, S4)
	if awg.S3 != nil || awg.S4 != nil {
		return Dialect20
	}

	// 3. Проверка параметров диалекта 1.5 (J1, J2, J3, Itime)
	if awg.J1 != nil || awg.J2 != nil || awg.J3 != nil || awg.Itime != nil {
		return Dialect15
	}

	// 4. Проверка параметров диалекта Classic (1.0)
	if awg.Jc != nil || awg.Jmin != nil || awg.Jmax != nil ||
		awg.S1 != nil || awg.S2 != nil ||
		awg.H1 != "" || awg.H2 != "" || awg.H3 != "" || awg.H4 != "" {
		return DialectClassic
	}

	return DialectPlain
}

// InferAWGVersion выполняет авто-инференс версии протокола AmneziaWG для узла,
// если версия не была явно задана в конфигурации.
// Возвращает эффективную версию ("3.1", "2.0", "1.5", "1.0" или "").
func InferAWGVersion(node *SubscriptionNode) string {
	if node == nil || node.AWG == nil || node.AWG.IsEmpty() {
		return ""
	}
	if node.AWG.Version != "" {
		return node.AWG.Version
	}
	dialect := DetectWireGuardDialect(node)
	switch dialect {
	case Dialect31:
		node.AWG.Version = "3.1"
		return "3.1"
	case Dialect20:
		node.AWG.Version = "2.0"
		return "2.0"
	case Dialect15:
		node.AWG.Version = "1.5"
		return "1.5"
	case DialectClassic:
		node.AWG.Version = "1.0"
		return "1.0"
	default:
		return ""
	}
}
