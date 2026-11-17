package proxyping

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type Limits struct {
	Parallel    int
	MinMemAvail uint64
	ChunkSize   int
	Class       string
}

// DefaultLimits — шов профиля мощности фазы 153. Значения до замера на
// устройстве — оценка пробы 143 Q8 (4), D-08.
func DefaultLimits(goarch string, ncpu int) Limits {
	if strings.HasPrefix(goarch, "mips") || ncpu < 2 {
		return Limits{2, 64 << 20, 64, "weak"}
	}
	return Limits{4, 64 << 20, 64, "normal"}
}

// MemAvailable возвращает доступную память в байтах; неизвестное значение
// отличается от нулевого, чтобы отсутствие поля не включало запасной путь.
func MemAvailable() (uint64, bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		v := strings.Fields(s.Text())
		if len(v) >= 2 && v[0] == "MemAvailable:" {
			n, err := strconv.ParseUint(v[1], 10, 64)
			return n * 1024, err == nil
		}
	}
	return 0, false
}
