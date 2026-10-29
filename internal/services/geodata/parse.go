package geodata

import (
	"regexp"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Итог по файлу.
const (
	StatusUpdated   = "updated"
	StatusInstalled = "installed"
	StatusKept      = "kept"
	StatusFailed    = "failed"
)

// Причины kept/failed у файла.
const (
	ReasonNoSize  = "no_size"
	ReasonHTML    = "html"
	ReasonSize    = "size"
	ReasonSHA256  = "sha256"
	ReasonNetwork = "network"
)

// Ошибки всего запуска (RunParse.RunError).
const (
	RunErrNetwork   = "run_network"
	RunErrNoSources = "no_sources"
)

// FileOutcome — итог запуска по одному файлу. Код возврата `xkeen -ug` почти всегда 0,
// поэтому итог берётся только из текста вывода.
//
//   - updated / installed — новый файл на месте; Unverified — XKeen поставил его без
//     проверки SHA-256 (GitHub API недоступен или не отдал сумму);
//   - kept — загрузка не удалась, оставлен старый файл (Reason — почему);
//   - failed — нового файла нет, старого тоже.
type FileOutcome struct {
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	Unverified bool   `json:"unverified,omitempty"`
}

// RunParse — разобранный вывод одного запуска. Parsed=false — формат вывода не
// узнан (XKeen другой версии): вызывающий показывает общий итог, вывод не теряется.
type RunParse struct {
	Files    map[string]FileOutcome
	GeoIPSet *FileOutcome
	RunError string
	Parsed   bool
}

// Маркеры вывода XKeen 2.0.1 (02_install/04_install_geofile.sh, 05_install_geoipset.sh,
// 04_tools/07_tools_downloaders/00_fetch_with_mirrors.sh, 05_tests/01_tests_system.sh).
var (
	parseBlockStartRe = regexp.MustCompile(`^\s*Запрос информации о (.+?)\.\.\.\s*$`)
	parseOKRe         = regexp.MustCompile(`^\s*(.+?) успешно (обновлён|установлен)\s*$`)
	parseKeptRe       = regexp.MustCompile(`Невозможно обновить (.+?)\. Оставляем старый файл`)
	parseFailNewRe    = regexp.MustCompile(`Невозможно загрузить (.+?)\s*$`)
	parseKeepNoSizeRe = regexp.MustCompile(`^\s*Оставляем старый файл\s+(.+?)\s*$`)
	parseMissingRe    = regexp.MustCompile(`старый файл (.+?) отсутствует`)
	parseRawErrRe     = regexp.MustCompile(`^\s*Ошибка: (html_stub|size|size_mismatch|content_v4|content_v6)\s*$`)
	parseNoSourcesRe  = regexp.MustCompile(`Не обнаружены базы`)
	parseNoNetRe      = regexp.MustCompile(`Отсутствует интернет-соединение`)
	parseGitHubDownRe = regexp.MustCompile(`^\s*Ошибка: GitHub недоступен\s*$`)
	parseProxyDownRe  = regexp.MustCompile(`^\s*Ошибка: Указанный вами прокси .+ недоступен\s*$`)
)

// parseBlock — блок файла, начатый строкой «Запрос информации о X...».
type parseBlock struct {
	display    string
	reason     string
	unverified bool
}

func (b *parseBlock) reasonOrNetwork() string {
	if b.reason != "" {
		return b.reason
	}
	return ReasonNetwork
}

// parseIsIPSet — блок списков GeoIPSET («IPv4 (IPSet)», «IPv6 (IPSet)», «GeoIPSET…»).
func parseIsIPSet(display string) bool {
	return strings.HasPrefix(display, "GeoIPSET") || strings.Contains(display, "(IPSet)")
}

func parseRank(s string) int {
	switch s {
	case StatusFailed:
		return 3
	case StatusKept:
		return 2
	}
	return 1
}

// parseMergeIPSet сводит блоки IPv4 и IPv6 в один итог: худший побеждает.
func parseMergeIPSet(old *FileOutcome, n FileOutcome) *FileOutcome {
	if old == nil {
		return &n
	}
	if parseRank(n.Status) > parseRank(old.Status) {
		return &n
	}
	if parseRank(n.Status) == parseRank(old.Status) && n.Unverified {
		old.Unverified = true
	}
	return old
}

// ParseUGOutput разбирает вывод `xkeen -ug` по файлам. displays — «название блока в
// выводе» → имя файла (стандартные наборы и записи geodata). Блок с неизвестным
// названием учитывается, только если это имя dat-файла или список GeoIPSET.
func ParseUGOutput(lines []string, displays map[string]string) RunParse {
	res := RunParse{Files: map[string]FileOutcome{}}
	var (
		cur       *parseBlock
		blocks    int
		noSources bool
		netBreak  bool
	)

	record := func(display string, o FileOutcome) {
		if name, ok := displays[display]; ok {
			res.Files[name] = o
			return
		}
		if parseIsIPSet(display) {
			res.GeoIPSet = parseMergeIPSet(res.GeoIPSet, o)
			return
		}
		if strings.HasSuffix(strings.ToLower(display), ".dat") {
			res.Files[display] = o
		}
	}
	// end закрывает текущий блок итоговой строкой; нет блока (начало вывода не
	// узнано) — итог берётся по названию из самой строки.
	end := func(display, status, reason string) {
		b := cur
		if b == nil {
			b = &parseBlock{display: display}
			blocks++
		}
		cur = nil
		o := FileOutcome{Status: status, Reason: reason}
		if status == StatusUpdated || status == StatusInstalled {
			o.Reason = ""
			o.Unverified = b.unverified
		}
		record(b.display, o)
	}

	for _, raw := range lines {
		line := strings.TrimSpace(utils.StripANSI(raw))
		if line == "" {
			continue
		}

		if m := parseBlockStartRe.FindStringSubmatch(line); m != nil {
			if cur != nil { // прошлый блок не завершён итоговой строкой
				end(cur.display, StatusFailed, cur.reasonOrNetwork())
			}
			cur = &parseBlock{display: m[1]}
			blocks++
			continue
		}
		if m := parseOKRe.FindStringSubmatch(line); m != nil {
			status := StatusUpdated
			if m[2] == "установлен" {
				status = StatusInstalled
			}
			end(m[1], status, "")
			continue
		}
		if m := parseKeptRe.FindStringSubmatch(line); m != nil {
			end(m[1], StatusKept, reasonOf(cur))
			continue
		}
		if m := parseFailNewRe.FindStringSubmatch(line); m != nil {
			end(m[1], StatusFailed, reasonOf(cur))
			continue
		}
		// Ветка без ожидаемого размера: загрузка пропущена, итог — одной из двух строк.
		if m := parseKeepNoSizeRe.FindStringSubmatch(line); m != nil {
			end(m[1], StatusKept, ReasonNoSize)
			continue
		}
		if m := parseMissingRe.FindStringSubmatch(line); m != nil {
			end(m[1], StatusFailed, ReasonNoSize)
			continue
		}

		switch {
		case parseNoSourcesRe.MatchString(line):
			noSources = true
			continue
		case parseNoNetRe.MatchString(line), parseGitHubDownRe.MatchString(line), parseProxyDownRe.MatchString(line):
			netBreak = true
			continue
		}

		if cur == nil {
			continue
		}
		switch {
		case strings.Contains(line, "Не удалось определить ожидаемый размер"):
			cur.reason = ReasonNoSize
		case strings.Contains(line, "БЕЗ проверки целостности"):
			cur.unverified = true
		case strings.Contains(line, "HTML-страница"):
			cur.reason = ReasonHTML
		case strings.Contains(line, "Размер загруженного файла") && strings.Contains(line, "не соответствует"):
			cur.reason = ReasonSize
		case strings.Contains(line, "SHA-256") || strings.Contains(line, "sha256sum"):
			switch {
			case strings.Contains(line, "Предупреждение"):
				// «не найдена в GitHub API» и подобное в режиме warn: файл поставлен без сверки.
				cur.unverified = true
			case strings.Contains(line, "проверена"):
			case strings.Contains(line, "не совпала"):
				// В режиме warn файл всё равно ставится; в strict следом идёт «не подтверждена».
				cur.unverified = true
				cur.reason = ReasonSHA256
			default:
				cur.reason = ReasonSHA256
			}
		case strings.Contains(line, "не содержит корректных IPv"):
			cur.reason = ReasonHTML // содержимое — не список адресов
		case strings.Contains(strings.ToLower(line), "не удалось загрузить"):
			cur.reason = ReasonNetwork
		default:
			if m := parseRawErrRe.FindStringSubmatch(line); m != nil {
				switch m[1] {
				case "html_stub", "content_v4", "content_v6":
					cur.reason = ReasonHTML
				default:
					cur.reason = ReasonSize
				}
			}
		}
	}
	if cur != nil {
		end(cur.display, StatusFailed, cur.reasonOrNetwork())
	}

	if blocks == 0 {
		switch {
		case noSources:
			res.RunError = RunErrNoSources
		case netBreak:
			res.RunError = RunErrNetwork
		}
	}
	res.Parsed = blocks > 0 || res.RunError != ""
	return res
}

// reasonOf — причина по последнему сообщению об ошибке внутри блока.
func reasonOf(b *parseBlock) string {
	if b == nil {
		return ReasonNetwork
	}
	return b.reasonOrNetwork()
}
