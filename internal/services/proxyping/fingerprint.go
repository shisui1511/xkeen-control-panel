package proxyping

import (
	"encoding/hex"
	"encoding/json"
	"hash/fnv"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
)

// Fingerprint учитывает параметры подключения, а не отображаемое имя.
// У неподдерживаемого Xray узла сохраняются все параметры исходной формы.
// Это признак изменения узла для кэша замеров в памяти, а не защита секрета:
// хеш не хранится и не передаётся, поэтому стойкость к подбору не нужна.
func Fingerprint(n nodes.Node) string {
	ob, err := nodes.XrayOutbound(n, "_")
	var data []byte
	if err == nil {
		data, err = json.Marshal(ob)
	}
	if err != nil {
		data, _ = json.Marshal(n)
	}
	h := fnv.New128a()
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
