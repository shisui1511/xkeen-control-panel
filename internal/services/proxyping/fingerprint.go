package proxyping

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
)

// Fingerprint учитывает параметры подключения, а не отображаемое имя.
// У неподдерживаемого Xray узла сохраняются все параметры исходной формы.
func Fingerprint(n nodes.Node) string {
	ob, err := nodes.XrayOutbound(n, "_")
	var data []byte
	if err == nil {
		data, err = json.Marshal(ob)
	}
	if err != nil {
		data, _ = json.Marshal(n)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
