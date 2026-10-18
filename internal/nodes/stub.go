package nodes

import "strings"

// zeroUUID — нулевой UUID: провайдер, отклонивший устройство (например, по
// лимиту HWID), подставляет его вместо учётных данных.
const zeroUUID = "00000000-0000-0000-0000-000000000000"

// IsStub сообщает, что узел — заглушка провайдера (D-14 из 133, D-26): адрес
// пустой, 0.0.0.0 или 127.0.0.1, порт не больше 1 либо нулевой UUID среди
// UUID и пароля. Имя узла не анализируется: «Истекла подписка» у рабочего
// узла заглушкой его не делает.
func IsStub(n Node) bool {
	switch strings.TrimSpace(n.Host) {
	case "", "0.0.0.0", "127.0.0.1":
		return true
	}
	if n.Port <= 1 {
		return true
	}
	for _, cred := range []string{n.UUID, n.Password} {
		if strings.EqualFold(strings.TrimSpace(cred), zeroUUID) {
			return true
		}
	}
	return false
}

// SplitStubs отделяет рабочие узлы от заглушек, порядок рабочих сохраняется.
func SplitStubs(list []Node) (working []Node, stubs int) {
	working = make([]Node, 0, len(list))
	for _, n := range list {
		if IsStub(n) {
			stubs++
			continue
		}
		working = append(working, n)
	}
	return working, stubs
}
