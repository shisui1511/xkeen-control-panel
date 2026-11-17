package routing

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Порядок запусков выбора Xray (G-150-3, D-17). Все ручные группы Xray пишут
// один файл xcp-groups, поэтому конвейер выбора идёт один за раз. Выборы
// независимых групп, пришедшие, пока идёт запуск, не отклоняются, а собираются в
// следующую пачку и применяются одним проходом xcp-groups: намерения пачки
// пишутся одной записью Live под замками применения до сборки (LiveRun.Prepare),
// поэтому сборка видит выборы всех групп пачки, а идущий запуск не захватывает в
// свой файл выбор ещё ждущей группы.
//
// Порядок замков: ход выбора (turn) → applyMu → lifecycleMu. Под applyMu и
// lifecycleMu ход выбора не берётся: хуки AfterApply запускаются после снятия
// замка применения, CommitLive выбор не вызывает. Связанные группы и та же группа
// сюда не доходят: их отсекает отметка идущего выбора (beginSelection) раньше
// ожидания хода, очереди нажатий в одной группе нет.

// xrayRunKind — вид запуска выбора Xray: у каждого вида своя собирающаяся пачка.
type xrayRunKind int

const (
	// xrayRunSelect — выбор участника: пачка намерений, запись в Prepare.
	xrayRunSelect xrayRunKind = 0
	// xrayRunConfirm — «Повторить» и сверка маркеров при старте: повторная запись
	// xcp-groups по сохранённому намерению, без новых выборов.
	xrayRunConfirm xrayRunKind = 1
)

// errXrayNothingToRun — в пачке не записано ни одного намерения: конвейер не
// запускается, исхода нет.
var errXrayNothingToRun = errors.New("routing: в пачке выбора нет намерений")

// xrayIntent — намерение одного выбора в пачке.
type xrayIntent struct {
	// Вход: группа, участник и связная компонента группы.
	gid, member string
	comp        []string
	// Выход записи (Prepare): прежнее значение выбора, поставлен ли маркер
	// неизвестного исхода на компоненту, записано ли намерение и ошибка до записи.
	prev    string
	had     bool
	pending bool
	wrote   bool
	err     error
	// Разбор исхода под ходом выбора (settleXrayBatch): ошибка возврата выбора и
	// ошибка записи маркера неизвестного исхода. Возврат идёт до освобождения хода,
	// чтобы следующая пачка не собрала xcp-groups с отвергнутым выбором (CR-01).
	restoreErr error
	markErr    error
	// joined — вступление в пачку; wait — от вступления до запечатывания пачки.
	joined time.Time
	wait   time.Duration
}

// xrayBatch — пачка запуска выбора Xray: намерения одного прохода конвейера и
// его итог, общий для всех участников пачки.
type xrayBatch struct {
	kind    xrayRunKind
	intents []*xrayIntent
	waiters int
	// done закрывается, когда итог пачки готов.
	done chan struct{}
	// Итог прохода: исход по Xray, причина перезапуска, исход шага, ошибка
	// запуска и состояние конвейера.
	out, reason, outcome string
	err                  error
	view                 configlayer.ApplyView
	// size — число записанных намерений; live — запись Live; run — весь RunLive;
	// hot — хук горячей замены.
	size           int
	live, run, hot time.Duration
	// markersCommitted — маркеры Xray компонент записанных намерений сняты записью
	// коммита прохода (CommitLive, 150-26 IO=B); false — коммита не было.
	markersCommitted bool
}

// xrayRunGate — один запуск выбора Xray за раз и собирающиеся пачки по видам.
// mu — листовой замок: под ним только next, waiters, список намерений, группы
// коммита и ленивое создание turn; ни проход конвейера, ни ожидание turn или done
// под mu не идут. Нулевое значение рабочее.
type xrayRunGate struct {
	mu sync.Mutex
	// next — собирающаяся (ещё не запечатанная) пачка каждого вида.
	next [2]*xrayBatch
	// turn — право запустить конвейер выбора (ёмкость 1): занят отправкой,
	// освобождается чтением.
	turn chan struct{}
	// commitComp — группы компонент намерений, записанных текущим проходом выбора
	// (заполняет Prepare, очищает держатель хода после RunLive): CommitLive запуска
	// выбора снимает их маркеры Xray той же записью, что фиксирует манифест, и
	// ставит committed (150-26, IO=B). Другой запуск выбора между Prepare и
	// очисткой не начнётся: ход держит этот проход.
	commitComp []string
	committed  bool
}

// runXray ставит намерение in (nil у «Повторить» и сверки) в собирающуюся пачку
// вида kind и ждёт её итога. Первый получивший ход запечатывает пачку и
// запускает её проход; остальные участники получают тот же итог. До
// запечатывания ожидание прерывают отмена ctx и остановка сервиса — намерение
// убирается из пачки; после запечатывания вызывающий ждёт итог (намерение могло
// быть записано, ему нужен исход).
func (s *Service) runXray(ctx context.Context, kind xrayRunKind, in *xrayIntent) (*xrayBatch, error) {
	g := &s.xrun
	g.mu.Lock()
	if g.turn == nil {
		g.turn = make(chan struct{}, 1)
	}
	turn := g.turn
	b := g.next[kind]
	if b == nil {
		b = &xrayBatch{kind: kind, done: make(chan struct{})}
		g.next[kind] = b
	}
	b.waiters++
	if in != nil {
		in.joined = time.Now()
		b.intents = append(b.intents, in)
	}
	g.mu.Unlock()

	var stop <-chan struct{}
	if s.ctx != nil {
		stop = s.ctx.Done()
	}
	var waitErr error
	select {
	case <-b.done:
		return b, nil
	case turn <- struct{}{}:
		g.mu.Lock()
		if g.next[kind] == b {
			g.next[kind] = nil
			now := time.Now()
			for _, x := range b.intents {
				x.wait = now.Sub(x.joined)
			}
			g.mu.Unlock()
			s.execXrayBatch(b)
			<-turn
			return b, nil
		}
		// Пачку запечатал другой: он держит ход до закрытия done, значит итог готов.
		g.mu.Unlock()
		<-turn
		<-b.done
		return b, nil
	case <-ctx.Done():
		waitErr = ctx.Err()
	case <-stop:
		waitErr = configlayer.ErrStopped
	}
	g.mu.Lock()
	if g.next[kind] == b {
		if in != nil {
			for i, x := range b.intents {
				if x == in {
					b.intents = append(b.intents[:i:i], b.intents[i+1:]...)
					break
				}
			}
		}
		b.waiters--
		if b.waiters == 0 {
			g.next[kind] = nil
		}
		g.mu.Unlock()
		return nil, waitErr
	}
	g.mu.Unlock()
	<-b.done
	return b, nil
}

// execXrayBatch выполняет проход конвейера пачки b. Вызывается только держателем
// хода; в конце всегда закрывает b.done.
func (s *Service) execXrayBatch(b *xrayBatch) {
	defer close(b.done)
	base := s.ctx
	if base == nil {
		base = context.Background()
	}
	// Ожидание замка применения ограничено: его держат «Применить», фоновый запуск
	// или операция с ядром, а не другие выборы.
	rctx, cancel := context.WithTimeout(base, selectBusyWait)
	defer cancel()

	hot := s.xrayHot
	run := configlayer.LiveRun{Trigger: configlayer.TriggerSelect, Kernel: configlayer.KernelXray}
	if b.kind == xrayRunConfirm {
		hot = s.xrayHotConfirm
		run.ReapplyKeys = []string{configlayer.ManifestKey(configlayer.KernelXray, xrayGroupsFile)}
	} else {
		run.Prepare = func() error { return s.prepareXrayBatch(b) }
		// Выбор меняет только тела исходящих xcp-groups: проверка Xray — по
		// изменённым исходящим, отказ — полная проверка конвейера (150-17, F4).
		run.XrayValidate = s.xrayValidateChanged
		// Выбор меняет только xcp-groups, остальные файлы Xray от выбора не зависят:
		// сборка — только генератором этого файла (150-24, F6); дрейф и проверки до
		// записи конвейер проводит полностью. «Повторить» (xrayRunConfirm) переписывает
		// файл целиком и собирает всё.
		run.BuildOnly = []string{xrayGenID}
	}
	run.XrayHot = func(hctx context.Context, files []configlayer.HotFile) error {
		start := time.Now()
		err := hot(hctx, files)
		b.hot = time.Since(start)
		return err
	}

	start := time.Now()
	view, err := s.d.Layer.RunLive(rctx, run)
	b.run = time.Since(start)
	b.view = view
	// Группы коммита — только этого прохода: следующий запуск их не снимет.
	g := &s.xrun
	g.mu.Lock()
	b.markersCommitted = g.committed
	g.commitComp, g.committed = nil, false
	g.mu.Unlock()
	switch {
	case err == nil:
		b.out, b.reason, b.outcome = s.xrayOutcome(view)
	case errors.Is(err, errXrayNothingToRun):
		// Ни одно намерение не записано: у каждого своя ошибка, исхода нет.
	case errors.Is(err, ErrApplyBusy) || (errors.Is(err, context.DeadlineExceeded) && base.Err() == nil):
		b.err = ErrApplyBusy
	default:
		b.out, b.err = selFailed, err
	}
	s.settleXrayBatch(b)
}

// settleXrayBatch разбирает исход пачки выбора под ходом, до освобождения хода и
// закрытия b.done: возврат выбора неудачного прохода и маркеры неизвестного
// исхода. Вне хода следующая пачка успела бы собрать xcp-groups с отвергнутым
// выбором чужой группы, и Live разошёлся бы с ядром (CR-01, D-17). Возврат касается
// только своей группы и только при неизменённом значении; результат каждого
// намерения — restoreErr и markErr, Select их читает.
func (s *Service) settleXrayBatch(b *xrayBatch) {
	if b.kind != xrayRunSelect {
		return
	}
	switch {
	case selectionOK(b.out) && b.err == nil:
		// Маркеры сняты записью коммита прохода (CommitLive); без коммита их снимает
		// Select (clearUnconfirmed) уже без хода: файлы совпадают с Live.
	case b.err != nil || xraySelectionReverted(b.outcome):
		for _, in := range b.intents {
			if in.wrote {
				in.restoreErr = s.restoreSelection(in.gid, in.member, in.prev, in.had, in.comp)
			}
		}
	default:
		// Неизвестный исход никогда не выдаётся за успешный возврат: компоненты
		// намерений без маркера из Prepare получают его здесь, а после коммита прохода
		// (он снял маркеры Prepare) — все записанные (замысел F5 плана 150-17).
		var comps [][]string
		for _, in := range b.intents {
			if in.wrote && (!in.pending || b.markersCommitted) {
				comps = append(comps, in.comp)
			}
		}
		if len(comps) == 0 {
			return
		}
		err := s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
			m := readUnconfirmed(live)
			for _, comp := range comps {
				for _, id := range comp {
					m[id] = configlayer.KernelXray
				}
			}
			return writeUnconfirmed(live, m)
		})
		for _, in := range b.intents {
			if in.wrote && (!in.pending || b.markersCommitted) {
				in.markErr = err
			}
		}
	}
}

// xrayValidateChanged — сокращённая проверка Xray запуска выбора
// (configlayer.XrayValidateFunc; бинарник и каталог — от конвейера): true только если план меняет ровно один файл
// xcp-groups (запись поверх существующего), набор тегов прежний (hotTags), есть
// изменённые исходящие и `xray run -test` по минимальному конфигу из одних этих
// исходящих завершился с кодом 0. Любая неприменимость или ошибка — false:
// конвейер выполняет полную проверку ValidateXray. Временный каталог (0700, файл
// 0600) удаляется сразу; в журнал — только число тегов и код выхода (тела несут
// учётные данные узлов).
func (s *Service) xrayValidateChanged(ctx context.Context, bin, tmpBase string, files []configlayer.HotFile) bool {
	if len(files) != 1 || files[0].RelPath != xrayGroupsFile || files[0].Action != configlayer.ActionWrite || !files[0].OldExisted {
		return false
	}
	if bin == "" || tmpBase == "" {
		return false
	}
	tags, next, _, err := hotTags(files[0].Old, files[0].New)
	if err != nil || len(tags) == 0 {
		return false
	}
	bodies := make([]json.RawMessage, 0, len(tags))
	for _, tag := range tags {
		bodies = append(bodies, next[tag])
	}
	data, err := json.Marshal(struct {
		Outbounds []json.RawMessage `json:"outbounds"`
	}{bodies})
	if err != nil {
		return false
	}
	// Каталог в <data_dir>/tmp с префиксом apply-, как у полной проверки: тела
	// исходящих несут учётные данные узлов, а остаток после падения процесса уберёт
	// configlayer.CleanupStale (в общем /tmp его никто не подметал бы).
	if err := os.MkdirAll(tmpBase, 0o700); err != nil {
		return false
	}
	dir, err := os.MkdirTemp(tmpBase, "apply-val-")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "outbounds.json"), data, 0600); err != nil {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, configlayer.ValidateTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "run", "-test", "-confdir", dir)
	cmd.Env = utils.XrayAssetEnv(os.Environ(), "")
	cmd.WaitDelay = configlayer.ValidateWaitDelay
	code := 0
	if err := cmd.Run(); err != nil {
		code = -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
	}
	log.Printf("[routing] проверка Xray выбора по изменённым исходящим: тегов %d, код %d", len(tags), code)
	return code == 0
}

// prepareXrayBatch пишет намерения пачки одной записью Live (под замками
// применения и жизненного цикла, до сборки): выбор каждой группы, маркер
// неизвестного исхода Xray на её компоненту при запущенном Xray и снятие группы
// из повторного выбора — то же, что setSelection. Группа с маркером получает
// ErrSelectionUnconfirmed и в запись не попадает. Компоненты записанных намерений
// становятся группами коммита хода (xrayRunGate.commitComp).
func (s *Service) prepareXrayBatch(b *xrayBatch) error {
	start := time.Now()
	running := s.d.Running(configlayer.KernelXray)
	type result struct {
		prev    string
		had     bool
		pending bool
		err     error
	}
	results := make([]result, len(b.intents))
	n := 0
	err := s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
		n = 0
		unconfirmed := readUnconfirmed(live)
		sel := liveSelection(live)
		repick := readRepick(live)
		hadRepick := len(repick) > 0
		for i, in := range b.intents {
			results[i] = result{}
			if unconfirmed[in.gid] != "" {
				results[i].err = ErrSelectionUnconfirmed
				continue
			}
			prev, had := sel[in.gid]
			results[i].prev, results[i].had = prev, had
			sel[in.gid] = in.member
			if running {
				for _, id := range in.comp {
					unconfirmed[id] = configlayer.KernelXray
				}
				results[i].pending = true
			}
			repick = without(repick, in.gid)
			n++
		}
		if n == 0 {
			// Нечего писать: запись на флешку не нужна.
			return errXrayNothingToRun
		}
		if err := writeSelection(live, sel); err != nil {
			return err
		}
		if err := writeUnconfirmed(live, unconfirmed); err != nil {
			return err
		}
		if hadRepick {
			return writeRepick(live, repick)
		}
		return nil
	})
	if err != nil && !errors.Is(err, errXrayNothingToRun) {
		for i, in := range b.intents {
			if results[i].err != nil {
				in.err = results[i].err
			} else {
				in.err = err
			}
		}
		return err
	}
	for i, in := range b.intents {
		in.prev, in.had, in.pending, in.err = results[i].prev, results[i].had, results[i].pending, results[i].err
		in.wrote = in.err == nil && err == nil
	}
	if err != nil {
		return err
	}
	// Группы, маркеры которых снимет запись коммита этого прохода (CommitLive).
	var comp []string
	for _, in := range b.intents {
		if in.wrote {
			comp = append(comp, in.comp...)
		}
	}
	g := &s.xrun
	g.mu.Lock()
	g.commitComp, g.committed = comp, false
	g.mu.Unlock()
	b.size = n
	b.live = time.Since(start)
	return nil
}

// xrayStages — замер одного выбора с путём Xray для строки этапов журнала: по
// ней план-сессия объясняет время выбора. В строку попадают только ID группы,
// коды и числа — без имён узлов, адресов и содержимого файлов (T-150-41).
type xrayStages struct {
	gid string
	// start — вход в Select; model — от входа до захвата компоненты; markers —
	// разбор исхода (снятие, возврат или запись маркеров).
	start          time.Time
	model, markers time.Duration
	// clash — ранний PUT Clash API быстрого пути; tail — фоновое продолжение после
	// ответа; answered — момент ответа быстрого пути (total — до него). На других
	// путях нули.
	clash, tail time.Duration
	answered    time.Time
	// reput — код повтора PUT Mihomo хвостом быстрого пути: reputSkip (Live равен
	// цели раннего PUT, повтора нет), reputDone (повтор выполнен); пусто — хвоста
	// быстрого пути не было (в строке — reputNone).
	reput string
	in    *xrayIntent
	b     *xrayBatch
	// err — ошибка ожидания хода (runXray).
	err error
	// syncN0, syncD0 — снимок utils.SyncStats при создании замера (вход в Select):
	// разность со снимком при строке — fsync синхронных записей процесса панели за
	// время выбора (поля fsync и fsyncs). Счётчики общие для процесса, поэтому в
	// разность входят и записи, идущие параллельно выбору.
	syncN0 int64
	syncD0 time.Duration
}

// Коды поля reput строки этапов: только коды, без имён узлов (T-150-41).
const (
	reputSkip = "skip"
	reputDone = "done"
	reputNone = "none"
)

// newXrayStages создаёт замер выбора группы gid со снимком счётчиков fsync.
func newXrayStages(gid string, start time.Time, model time.Duration, in *xrayIntent) *xrayStages {
	n, d := utils.SyncStats()
	return &xrayStages{gid: gid, start: start, model: model, in: in, syncN0: n, syncD0: d}
}

// stepMS — длительность шага конвейера id в итоге прохода, мс.
func stepMS(view configlayer.ApplyView, id configlayer.StepID) int64 {
	for _, st := range view.Steps {
		if st.ID == id {
			return st.DurationMS
		}
	}
	return 0
}

// outcome — исход выбора для строки этапов: исход прохода, если намерение
// записано, иначе код ошибки.
func (st *xrayStages) outcome() string {
	if st.b != nil && st.in != nil && st.in.wrote && st.b.out != "" {
		return st.b.out
	}
	err := st.err
	if err == nil && st.in != nil {
		err = st.in.err
	}
	if err == nil && st.b != nil {
		err = st.b.err
	}
	switch {
	case errors.Is(err, ErrApplyBusy):
		return "apply_busy"
	case errors.Is(err, configlayer.ErrStopped), errors.Is(err, context.Canceled):
		return "stopped"
	case errors.Is(err, ErrSelectionUnconfirmed):
		return "selection_unconfirmed"
	}
	return selFailed
}

// log пишет строку этапов: одна на выбор с путём Xray в любом исходе, целые мс.
// other — время RunLive вне пяти шагов конвейера (замки, Prepare, коммит, хвост).
func (st *xrayStages) log() {
	var (
		batch                                       int
		wait, live, hot, other                      time.Duration
		build, valX, valM, write, restart, stepsSum int64
	)
	if st.in != nil {
		wait = st.in.wait
	}
	if b := st.b; b != nil {
		batch, live, hot = b.size, b.live, b.hot
		build = stepMS(b.view, configlayer.StepBuild)
		valX = stepMS(b.view, configlayer.StepValidateXray)
		valM = stepMS(b.view, configlayer.StepValidateMihomo)
		write = stepMS(b.view, configlayer.StepWrite)
		restart = stepMS(b.view, configlayer.StepRestart)
		stepsSum = build + valX + valM + write + restart
		if d := b.run - time.Duration(stepsSum)*time.Millisecond; d > 0 {
			other = d
		}
	}
	// total — от входа в Select до ответа: на быстром пути ответ раньше строки.
	total := time.Since(st.start)
	if !st.answered.IsZero() {
		total = st.answered.Sub(st.start)
	}
	// fsync, fsyncs — ожидание записи на носитель синхронными записями панели
	// (utils.AtomicWriteFile) от входа в Select до этой строки.
	syncN, syncD := utils.SyncStats()
	reput := st.reput
	if reput == "" {
		reput = reputNone
	}
	log.Printf("[routing] выбор Xray: этапы group=%s batch=%d wait=%d model=%d live=%d build=%d validate_xray=%d validate_mihomo=%d write=%d restart=%d hot=%d other=%d markers=%d clash=%d tail=%d fsync=%d fsyncs=%d total=%d outcome=%s reput=%s",
		st.gid, batch, wait.Milliseconds(), st.model.Milliseconds(), live.Milliseconds(),
		build, valX, valM, write, restart, hot.Milliseconds(), other.Milliseconds(),
		st.markers.Milliseconds(), st.clash.Milliseconds(), st.tail.Milliseconds(),
		(syncD - st.syncD0).Milliseconds(), syncN-st.syncN0, total.Milliseconds(), st.outcome(), reput)
}
