package geodata

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Действия над строкой cron XKeen (CronAction).
const (
	CronAdopt  = "adopt"
	CronKeep   = "keep"
	CronRemove = "remove"
	CronGive   = "give"
)

// Коды отказов расписания и cron (общий lists.ErrorCode).
var (
	// ErrScheduleInvalid — час, минута или день расписания вне границ.
	ErrScheduleInvalid = &lists.CodedError{Code: "schedule_invalid"}
	// ErrCronMissing — в crontab нет строки `xkeen -ug`.
	ErrCronMissing = &lists.CodedError{Code: "cron_missing"}
	// ErrCronNonstandard — строка есть, но не в формате XKeen (`M H * * D`): не перенимается.
	ErrCronNonstandard = &lists.CodedError{Code: "cron_nonstandard"}
	// ErrCronFailed — crontab не прочитан или не записан, либо менялся между чтением и записью.
	ErrCronFailed = &lists.CodedError{Code: "cron_failed"}
	// ErrCronAction — неизвестное действие.
	ErrCronAction = &lists.CodedError{Code: "bad_op"}
)

// cronCmd — команда обновления баз в crontab XKeen.
const cronCmd = "/opt/sbin/xkeen -ug"

// reXKeenCron — стандартная строка XKeen: `M H * * D /opt/sbin/xkeen -ug`, D — цифра
// дня недели или `*` (ежедневно).
var reXKeenCron = regexp.MustCompile(`^\s*(\d{1,2})\s+(\d{1,2})\s+\*\s+\*\s+([0-6*])\s+/opt/sbin/xkeen\s+-ug\s*$`)

// reUGCmd — любая строка с командой `xkeen -ug` как отдельным словом (не -ugi, -ugs, -ugc).
var reUGCmd = regexp.MustCompile(`/opt/sbin/xkeen\s+-ug(?:\s|$)`)

// cronLine строит строку XKeen по расписанию: только из чисел, пользовательский текст
// в crontab не попадает.
func cronLine(sch ScheduleState) string {
	dow := "*"
	if sch.Day >= 0 && sch.Day <= 6 {
		dow = strconv.Itoa(sch.Day)
	}
	return fmt.Sprintf("%d %d * * %s %s", sch.Minute, sch.Hour, dow, cronCmd)
}

// cronInfo — прочитанный crontab: содержимое и первая строка `xkeen -ug`.
type cronInfo struct {
	content  string
	mtime    time.Time
	size     int64
	exists   bool
	found    bool
	standard bool
	line     string // строка как в файле, без перевода строки
	sched    ScheduleState
}

// isUGLine — строка crontab запускает `xkeen -ug` (комментарии не считаются).
func isUGLine(line string) bool {
	t := strings.TrimSpace(line)
	return t != "" && !strings.HasPrefix(t, "#") && reUGCmd.MatchString(t)
}

// parseCronLine разбирает стандартную строку XKeen: расписание и признак пригодности.
func parseCronLine(line string) (ScheduleState, bool) {
	m := reXKeenCron.FindStringSubmatch(line)
	if m == nil {
		return ScheduleState{}, false
	}
	minute, _ := strconv.Atoi(m[1])
	hour, _ := strconv.Atoi(m[2])
	if minute > 59 || hour > 23 {
		return ScheduleState{}, false
	}
	day := -1
	if m[3] != "*" {
		day, _ = strconv.Atoi(m[3])
	}
	return ScheduleState{Day: day, Hour: hour, Minute: minute}, true
}

// cronText — расписание строки до команды (`30 5 * * *`).
func cronText(line string) string {
	if i := strings.Index(line, "/opt/sbin/xkeen"); i >= 0 {
		return strings.TrimSpace(line[:i])
	}
	return strings.TrimSpace(line)
}

// readCron читает crontab. Нет файла — не ошибка: пустое содержимое, exists=false.
func (s *Service) readCron() (cronInfo, error) {
	var info cronInfo
	fi, err := os.Stat(s.d.CronFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return info, nil
		}
		return info, err
	}
	data, err := os.ReadFile(s.d.CronFile)
	if err != nil {
		return info, err
	}
	info.exists = true
	info.content = string(data)
	info.mtime = fi.ModTime()
	info.size = fi.Size()
	for _, l := range strings.Split(info.content, "\n") {
		if !isUGLine(l) {
			continue
		}
		info.found = true
		info.line = strings.TrimRight(l, "\r")
		info.sched, info.standard = parseCronLine(info.line)
		break
	}
	return info, nil
}

// withoutUGLines убирает из содержимого все строки `xkeen -ug`.
func withoutUGLines(content string) string {
	var out []string
	for _, l := range strings.Split(content, "\n") {
		if isUGLine(l) {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// withLine дописывает строку в конец содержимого.
func withLine(content, line string) string {
	content = strings.TrimRight(content, "\n")
	if content != "" {
		content += "\n"
	}
	return content + line + "\n"
}

// writeCron заменяет содержимое crontab атомарно с прежним режимом файла. Перед
// записью файл перечитывается: изменился с момента чтения — ErrCronFailed. Пустые
// строки убираются (как `sed '/^$/d'` у XKeen). crond не перезапускается: XKeen
// правит файл так же.
func (s *Service) writeCron(old, content string) error {
	cur, err := os.ReadFile(s.d.CronFile)
	switch {
	case err == nil:
		if string(cur) != old {
			return ErrCronFailed
		}
	case errors.Is(err, os.ErrNotExist):
		if old != "" {
			return ErrCronFailed
		}
	default:
		return ErrCronFailed
	}
	var lines []string
	for _, l := range strings.Split(content, "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	data := ""
	if len(lines) > 0 {
		data = strings.Join(lines, "\n") + "\n"
	}
	if err := utils.AtomicReplaceFile(s.d.CronFile, []byte(data)); err != nil {
		return ErrCronFailed
	}
	return nil
}

// syncCron пересчитывает CronTwo и CronSchedule по crontab. Без force читает файл
// только если изменились время правки или размер.
func (s *Service) syncCron(force bool) {
	fi, statErr := os.Stat(s.d.CronFile)
	s.mu.Lock()
	if !force && s.sched.cronSeen {
		same := false
		if statErr != nil {
			same = s.sched.cronMissing
		} else {
			same = !s.sched.cronMissing && fi.ModTime().Equal(s.sched.cronMtime) && fi.Size() == s.sched.cronSize
		}
		if same {
			s.mu.Unlock()
			return
		}
	}
	s.mu.Unlock()

	info, err := s.readCron()
	if err != nil {
		return
	}
	s.mu.Lock()
	s.sched.cronSeen = true
	s.sched.cronMissing = !info.exists
	s.sched.cronMtime = info.mtime
	s.sched.cronSize = info.size
	two := info.found && s.st.Schedule.Enabled && !s.st.Suspended
	text := ""
	if info.found {
		text = cronText(info.line)
	}
	dirty := two != s.st.CronTwo || text != s.st.CronSchedule
	s.st.CronTwo = two
	s.st.CronSchedule = text
	if info.found && s.st.CronKept != "" && s.st.CronKept != info.line {
		s.st.CronKept = ""
		dirty = true
	}
	if !info.found && s.st.CronKept != "" {
		s.st.CronKept = ""
		dirty = true
	}
	s.mu.Unlock()
	if dirty {
		s.changed()
	}
}

// CronAction выполняет выбор владельца по строке cron XKeen: adopt — перенять
// расписание панели (строка убирается и запоминается), keep — оставить как есть,
// remove — убрать строку, не трогая расписание панели, give — отдать расписание
// обратно XKeen (расписание панели выключается, строка возвращается).
func (s *Service) CronAction(action string) error {
	s.sched.cronMu.Lock()
	defer s.sched.cronMu.Unlock()
	info, err := s.readCron()
	if err != nil {
		return ErrCronFailed
	}
	switch action {
	case CronAdopt:
		if !info.found {
			return ErrCronMissing
		}
		if !info.standard {
			return ErrCronNonstandard
		}
		if err := s.writeCron(info.content, withoutUGLines(info.content)); err != nil {
			return err
		}
		s.mu.Lock()
		s.st.CronTaken = &CronTaken{Line: info.line, At: s.d.Now()}
		s.st.Schedule = ScheduleState{Enabled: true, Day: info.sched.Day, Hour: info.sched.Hour, Minute: info.sched.Minute}
		s.st.Suspended = false
		s.st.CronKept = ""
		s.st.CronTwo = false
		s.markSlotLocked()
		s.mu.Unlock()
	case CronKeep:
		if !info.found {
			return ErrCronMissing
		}
		s.mu.Lock()
		s.st.CronKept = info.line
		s.mu.Unlock()
	case CronRemove:
		if !info.found {
			return ErrCronMissing
		}
		if err := s.writeCron(info.content, withoutUGLines(info.content)); err != nil {
			return err
		}
		s.mu.Lock()
		s.st.CronTwo = false
		s.st.CronKept = ""
		s.mu.Unlock()
	case CronGive:
		s.mu.Lock()
		sch := s.st.Schedule
		s.mu.Unlock()
		if !info.found {
			if err := s.writeCron(info.content, withLine(info.content, cronLine(sch))); err != nil {
				return err
			}
		}
		s.mu.Lock()
		s.st.Schedule.Enabled = false
		s.st.Suspended = false
		s.st.CronTaken = nil
		s.st.CronKept = ""
		s.st.CronTwo = false
		s.st.RetryAt = time.Time{}
		s.mu.Unlock()
	default:
		return ErrCronAction
	}
	s.changed()
	s.syncCron(true)
	return nil
}
