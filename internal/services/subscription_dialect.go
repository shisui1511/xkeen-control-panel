package services

// WireGuardDialect представляет тип/диалект конфигурации WireGuard/AmneziaWG.
type WireGuardDialect string

const (
	DialectPlain   WireGuardDialect = "plain"
	DialectClassic WireGuardDialect = "classic"
	Dialect20      WireGuardDialect = "2.0"
	Dialect31      WireGuardDialect = "3.1"
)

// DetectWireGuardDialect определяет диалект WireGuard/AmneziaWG узла:
// - "plain": чистый WireGuard без параметров обфускации;
// - "classic": AmneziaWG 1.0 (Jc, Jmin, Jmax, S1, S2, H1-H4);
// - "2.0": AmneziaWG 2.0 (наличие S3 или S4);
// - "3.1": AmneziaWG 3.1 (Version, HeaderProtectionKey, I1-I5, ContentPaddingAddition, RandomTrailers, DisableCookies, RekeyAfterTime или RawOptions).
func DetectWireGuardDialect(node *SubscriptionNode) WireGuardDialect {
	if node == nil || node.AWG == nil || node.AWG.IsEmpty() {
		return DialectPlain
	}

	awg := node.AWG

	// 1. Проверка параметров диалекта 3.1 (наивысший приоритет)
	if awg.Version != "" ||
		awg.HeaderProtectionKey != "" ||
		awg.I1 != "" || awg.I2 != "" || awg.I3 != "" || awg.I4 != "" || awg.I5 != "" ||
		awg.ContentPaddingAddition != nil ||
		awg.RandomTrailers != nil ||
		awg.DisableCookies != nil ||
		awg.RekeyAfterTime != nil ||
		len(awg.RawOptions) > 0 {
		return Dialect31
	}

	// 2. Проверка параметров диалекта 2.0 (S3, S4)
	if awg.S3 != nil || awg.S4 != nil {
		return Dialect20
	}

	// 3. Проверка параметров диалекта Classic (1.0)
	if awg.Jc != nil || awg.Jmin != nil || awg.Jmax != nil ||
		awg.S1 != nil || awg.S2 != nil ||
		awg.H1 != "" || awg.H2 != "" || awg.H3 != "" || awg.H4 != "" {
		return DialectClassic
	}

	return DialectPlain
}
