import { atom, read, update } from 'claude-code'
import type { Register } from 'claude-code'

import type { Block, Progress, Snapshot } from '../types'

// Панель хода прогона на стенде: раз в POLL_MS вызывает scripts/router/progress.mjs --json
// рабочей копии сессии и рисует этап, тесты идущего запуска, упавшие и оставшееся время.
// Строка состояния — progress.mjs --line, пока прогон идёт. Панель открывается сама, когда
// прогон начинается; вручную — /router-watch. Плагин ставится на пользователя и работает в любом
// каталоге, а progress.mjs он исполняет: поэтому опрос идёт только в рабочей копии этого
// репозитория (корневой коммит истории совпадает с ROOT_COMMIT), в остальных каталогах код
// репозитория не запускается, панель говорит, что хода прогона здесь нет.

const PANE = 'router-watch'
const TITLE = 'Прогон стенда'
const POLL_MS = 3000
// без идущего прогона опрос реже: раз в IDLE_TICKS тиков (15 с)
const IDLE_TICKS = 5
const SCRIPT = 'scripts/router/progress.mjs'
// корневой коммит xkeen-control-panel: признак «это наш репозиторий» (содержимое по хэшу, имя
// каталога и путь подделать проще)
const ROOT_COMMIT = 'ad34d2f7f1d2d155d2d624a3c304e39c9bf900df'
const FAILS_SHOWN = 15

const snapshot = atom({ plugin: 'router-watch', key: 'snapshot' } as const, { progress: null, error: '', at: 0 } as Snapshot, {
  shape: 'snapshot-1',
})

function dur(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec)) return '—'
  const s = Math.max(0, Math.round(sec))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (h) return `${h} ч ${String(m).padStart(2, '0')} мин`
  if (m) return `${m} мин ${String(s % 60).padStart(2, '0')} с`
  return `${s} с`
}

function bar(frac: number, width: number): string {
  const n = Math.max(0, Math.min(width, Math.round(frac * width)))
  return '█'.repeat(n) + '░'.repeat(width - n)
}

// часы для чисел, что меняются на глазах: 3:28, 1:05:12
function clock(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec)) return '—'
  const s = Math.max(0, Math.round(sec))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const ss = String(s % 60).padStart(2, '0')
  return h ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`
}

// полоса для графических поверхностей: ширина разметки с запасом — Svg сожмётся до ширины панели,
// высота задаётся явно, поэтому полоса остаётся тонкой при любой ширине
const BAR_W = 1000
const FILL = { run: '#4c8df6', warn: '#d29922' } as const
function barSvg(frac: number, fill: string): string {
  const f = Math.max(0, Math.min(1, frac))
  const w = Math.round(f * BAR_W)
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" width="${BAR_W}" height="8" viewBox="0 0 ${BAR_W} 8" preserveAspectRatio="none">` +
    `<rect width="${BAR_W}" height="8" rx="4" fill="#8b949e" fill-opacity="0.22"/>` +
    (w > 0 ? `<rect width="${Math.max(w, 8)}" height="8" rx="4" fill="${fill}"/>` : '') +
    `</svg>`
  )
}

let pollNow: () => void = () => {}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)
    await $.command.register({
      name: 'router-watch',
      description: 'Ход прогона роутерных тестов в панели',
    })
    // скрипт — от корня рабочей копии: сессия может начаться в подкаталоге
    const top = await $.process.run(['git', 'rev-parse', '--show-toplevel']).then(
      r => (r.exitCode === 0 ? r.stdout.trim() : ''),
      () => ''
    )
    const script = top ? `${top}/${SCRIPT}` : SCRIPT
    const ours = top
      ? await $.process.run(['git', '-C', top, 'rev-list', '--max-parents=0', 'HEAD']).then(
          r => r.exitCode === 0 && r.stdout.split('\n').some(l => l.trim() === ROOT_COMMIT),
          () => false
        )
      : false
    const hasScript =
      ours &&
      (await $.fs.stat(script).then(
        () => true,
        () => false
      ))
    if (!hasScript) {
      await update($, snapshot, () => ({
        progress: null,
        error: ours ? `в этой рабочей копии нет ${SCRIPT}` : 'это не рабочая копия xkeen-control-panel: ход прогона не опрашивается',
        at: Date.now(),
      }))
      return started
    }

    let wasRunning = false
    let active = false
    let tick = 0
    let busy = false
    const poll = async () => {
      tick++
      if (!active && tick % IDLE_TICKS !== 1) return
      if (busy) return
      busy = true
      try {
        const r = await $.process.run(['node', script, '--json'], {
          timeoutMs: 20_000,
        })
        if (r.exitCode !== 0) {
          // сбой опроса: строка состояния и признак «шёл» не остаются от прошлого снимка
          $.ui.status(undefined)
          wasRunning = false
          active = false
          await update($, snapshot, () => ({
            progress: null,
            error: r.stderr.trim().slice(0, 300) || `код ${r.exitCode}`,
            at: Date.now(),
          }))
          return
        }
        const progress = JSON.parse(r.stdout) as Progress
        await update($, snapshot, () => ({
          progress,
          error: '',
          at: Date.now(),
        }))
        const running = progress.state === 'running'
        active = running || progress.state === 'hold' || progress.queue.length > 0
        $.ui.status(running || progress.state === 'hold' ? progress.line : undefined)
        if (running && !wasRunning) void $.ui.open({ id: PANE, title: TITLE })
        // прогон закончился (итог или удержание стенда): тост и уведомление с итогом
        if (wasRunning && !running && progress.state !== 'none') {
          const verdict = progress.state === 'stopped' ? 'оборван' : progress.verdict || 'закончен'
          const text = `Прогон ${progress.suite || ''}: ${verdict}`
          $.ui.toast(text)
          void $.ui.notify(text, { title: TITLE })
        }
        wasRunning = running
      } catch (err) {
        $.ui.status(undefined)
        wasRunning = false
        active = false
        await update($, snapshot, () => ({
          progress: null,
          error: String(err).slice(0, 300),
          at: Date.now(),
        }))
      } finally {
        busy = false
      }
    }
    void poll()
    $.clock.every(POLL_MS, () => void poll())
    // открыть панель вручную — свежий снимок сразу, не дожидаясь редкого опроса
    pollNow = () => {
      tick = 0
      void poll()
    }
    return started
  })

  on('command.run', { command: 'router-watch' }, async $ => {
    pollNow()
    await $.ui.open({ id: PANE, title: TITLE })
    return { text: 'Панель хода прогона открыта.' }
  })

  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const els = $.ui.resolve(e)
    const { Box, Text } = els
    const Svg = 'Svg' in els ? els.Svg : null
    const snap = await read($, snapshot)
    const cols = (e.props as { bodyColumns?: number }).bodyColumns ?? e.viewport?.columns ?? 60
    const width = Math.max(10, Math.min(40, cols - 14))
    const p = snap.progress

    // полоса прогресса: на desktop/vscode/mobile — Svg, в терминале — символы
    const Bar = (frac: number, fill: string, alt: string) =>
      Svg ? <Svg source={barSvg(frac, fill)} alt={alt} height={8} /> : <Text>{bar(frac, width)}</Text>
    // плашка счётчика: нулевые приглушены
    const Pill = (label: string, n: number, color: string) => (
      <Box borderStyle="round" borderColor={n ? color : 'inactive'} paddingX={1} flexDirection="row" gap={1}>
        <Text size="caption" color={n ? color : 'inactive'} bold>
          {label}
        </Text>
        <Text tabular bold color={n ? undefined : 'inactive'}>
          {String(n)}
        </Text>
      </Box>
    )

    if (!p) {
      return (
        <Box flexDirection="column">
          <Text dimColor>{snap.error ? `Ход прогона недоступен: ${snap.error}` : 'Читаю ход прогона…'}</Text>
        </Box>
      )
    }
    const waiting = [
      ...p.queue.map(q => `В очереди на стенд: ${q.suite} — ждёт ${dur(q.sinceSec)}`),
      ...(p.unattended ? [`Ночной прогон ждёт старта${p.unattended.at ? ` в ${p.unattended.at}` : ''}`] : []),
    ]
    const waitingBox = waiting.length > 0 && (
      <Box flexDirection="column" borderStyle="round" borderColor="warning" paddingX={1}>
        {waiting.map(w => (
          <Text color="warning">{`⏳ ${w}`}</Text>
        ))}
      </Box>
    )
    if (p.state === 'none') {
      return (
        <Box flexDirection="column" gap={1}>
          {waitingBox}
          <Text dimColor>Прогонов на стенде нет.</Text>
        </Box>
      )
    }

    const running = p.state === 'running'
    const holding = p.state === 'hold'
    const red = /красный|отказ/.test(p.verdict)
    const failed = p.counts.FAIL > 0 || p.fails.length > 0
    const stateText = running ? 'идёт' : holding ? 'стенд удержан' : p.state === 'stopped' ? 'оборван' : 'закончен'
    const stateColor = running
      ? 'suggestion'
      : holding
        ? 'warning'
        : p.state === 'stopped'
          ? 'warning'
          : red
            ? 'error'
            : 'success'
    const fill = p.overSec > 0 ? FILL.warn : FILL.run
    const until = new Date(p.holdUntil * 1000)
    const untilText = `${String(until.getHours()).padStart(2, '0')}:${String(until.getMinutes()).padStart(2, '0')}`
    const blocks = p.blocks
    const several = blocks.length > 1
    const countsOf = (b: Block) =>
      Object.entries(b.counts)
        .filter(([, v]) => v)
        .map(([k, v]) => `${k} ${v}`)
        .join(' · ')
    const fails = p.fails.slice(-FAILS_SHOWN)

    // «проход X на ядре, спек путь → build/router/<отчёт>/<папка>»: отчёт уже в заголовке, оставить папку
    const [sessWhat = '', sessDir = ''] = p.session.split(' → ')
    const sessFolder = sessDir.replace(`build/router/${p.name}/`, '')
    const [sessPass, sessSpec = ''] = sessWhat.split(', спек ')

    // пока прогон идёт — доля от оценки и оставшееся; после — длительность
    const showPct = (running || holding) && p.percent != null
    const sideLine = [
      showPct ? `прошло ${clock(p.elapsedSec)}` : '',
      running && p.overSec > 0
        ? `дольше оценки на ${clock(p.overSec)}`
        : running && p.etaSec != null
          ? `осталось ~${clock(p.etaSec)}`
          : '',
      p.estSec ? `оценка ${clock(p.estSec)}` : '',
      running && p.forecastSec && p.estSec && Math.abs(p.forecastSec - p.estSec) >= 30
        ? `прогноз ${clock(p.forecastSec)}`
        : '',
    ]
      .filter(Boolean)
      .join('  ·  ')

    return (
      <Box flexDirection="column" gap={1}>
        {waitingBox}

        <Box flexDirection="column">
          <Box flexDirection="row" gap={1} alignItems="center">
            <Text size="heading" bold>
              {p.suite || 'прогон'}
            </Text>
            <Text color={stateColor} bold>{`● ${stateText}`}</Text>
          </Box>
          <Text size="caption" dimColor mono wrap="truncate-end">
            {p.name}
          </Text>
          {!running && p.verdict && <Text color={red ? 'error' : 'success'} bold>{`Итог: ${p.verdict}`}</Text>}
        </Box>

        <Box flexDirection="column">
          <Box flexDirection="row" justifyContent="space-between" alignItems="flex-end" gap={1}>
            <Text size="title" bold tabular>
              {showPct ? `${p.percent}%` : clock(p.elapsedSec)}
            </Text>
            <Text
              size="caption"
              color={running && p.overSec > 0 ? 'warning' : undefined}
              dimColor={!(running && p.overSec > 0)}
              tabular
              wrap="truncate-start"
            >
              {sideLine}
            </Text>
          </Box>
          {showPct && Bar((p.percent ?? 0) / 100, fill, `Прогон: ${p.percent}%`)}
          {running && p.idleSec > 300 && <Text size="caption" color="warning">{`Нет новых событий ${dur(p.idleSec)}`}</Text>}
        </Box>

        {holding && <Text color="warning">{`Стенд удержан для ручной сессии до ${untilText} (ещё ${dur(p.holdLeftSec)})`}</Text>}

        {p.session && (
          <Box flexDirection="column">
            <Text size="footnote">
              <Text dimColor>{'Сессия: '}</Text>
              {sessPass}
            </Text>
            {sessSpec && (
              <Text size="caption" dimColor mono wrap="truncate-start">
                {sessSpec}
              </Text>
            )}
            {sessFolder && (
              <Text size="caption" dimColor mono wrap="truncate-start">
                {`→ ${sessFolder}`}
              </Text>
            )}
          </Box>
        )}

        {(running || holding) && (p.stages.length > 0 || blocks.length > 0) && (
          <Box flexDirection="column" borderStyle="round" borderColor="inactive" paddingX={1}>
            {p.stages.map(st => (
              <Box flexDirection="row" justifyContent="space-between" gap={1}>
                <Text size="footnote">{`${several ? `${st.target} · ` : ''}${st.core !== '-' ? `${st.core} · ` : ''}${st.name}`}</Text>
                <Text size="caption" dimColor tabular>
                  {clock(st.sinceSec)}
                </Text>
              </Box>
            ))}
            {blocks.map(b => (
              <Box flexDirection="column">
                <Box flexDirection="row" justifyContent="space-between" gap={1}>
                  <Text bold>{`${several ? `${b.target} · ` : ''}${b.core !== '-' ? `${b.core} · ${b.name}` : b.name}`}</Text>
                  <Text tabular dimColor>
                    {`${b.done}/${b.total}${b.etaSec != null ? `  ·  ~${clock(b.etaSec)}` : ''}`}
                  </Text>
                </Box>
                {Bar(b.total ? b.done / b.total : 0, FILL.run, `${b.name}: ${b.done} из ${b.total}`)}
                {countsOf(b) && (
                  <Text size="caption" dimColor>
                    {countsOf(b)}
                  </Text>
                )}
                {b.current && (
                  <Text size="caption" dimColor wrap="truncate-end">
                    {`▸ ${b.current}${b.currentSec != null ? `  ·  ${dur(b.currentSec)}` : ''}`}
                  </Text>
                )}
              </Box>
            ))}
          </Box>
        )}

        <Box flexDirection="column">
          <Text size="caption" dimColor>
            {holding ? 'Ручная сессия' : 'Записано'}
          </Text>
          <Box flexDirection="row" flexWrap="wrap" gap={1}>
            {Pill('PASS', p.counts.PASS, 'success')}
            {Pill('FAIL', p.counts.FAIL, 'error')}
            {Pill('KNOWN', p.counts.KNOWN, 'warning')}
            {Pill('SKIP', p.counts.SKIP, 'subtle')}
          </Box>
        </Box>

        {fails.length > 0 && (
          <Box flexDirection="column" borderStyle="round" borderColor={failed ? 'error' : 'inactive'} paddingX={1}>
            <Text color="error" bold>{`Упало · ${p.fails.length}`}</Text>
            {fails.map(f => (
              <Box flexDirection="row" gap={1}>
                <Text color={f.live ? 'warning' : 'error'}>✗</Text>
                {f.core !== '-' && (
                  <Text size="caption" mono dimColor>
                    {f.target ? `${f.target} · ${f.core}` : f.core}
                  </Text>
                )}
                <Box flexDirection="column" flexShrink={1}>
                  <Text size="footnote">{f.name}</Text>
                  {f.detail && (
                    <Text size="caption" dimColor wrap="truncate-end">
                      {f.detail}
                    </Text>
                  )}
                </Box>
              </Box>
            ))}
            {p.fails.length > FAILS_SHOWN && <Text dimColor>{`… и ещё ${p.fails.length - FAILS_SHOWN}`}</Text>}
          </Box>
        )}
      </Box>
    )
  })
}
