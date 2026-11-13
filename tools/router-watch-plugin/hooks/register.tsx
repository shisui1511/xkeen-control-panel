import { atom, read, update } from 'claude-code'
import type { Register } from 'claude-code'

import type { Progress, Snapshot } from '../types'

// Панель хода прогона на стенде: раз в POLL_MS вызывает scripts/router/progress.mjs --json
// рабочей копии сессии и рисует этап, тесты идущего запуска, упавшие и оставшееся время.
// Строка состояния — progress.mjs --line, пока прогон идёт. Панель открывается сама, когда
// прогон начинается; вручную — /router-watch. В рабочей копии без progress.mjs (другой проект)
// опроса нет: панель говорит, что хода прогона здесь нет.

const PANE = 'router-watch'
const TITLE = 'Прогон стенда'
const POLL_MS = 3000
// без идущего прогона опрос реже: раз в IDLE_TICKS тиков (15 с)
const IDLE_TICKS = 5
const SCRIPT = 'scripts/router/progress.mjs'
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

let pollNow: () => void = () => {}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)
    await $.command.register({ name: 'router-watch', description: 'Ход прогона роутерных тестов в панели' })
    // скрипт — от корня рабочей копии: сессия может начаться в подкаталоге
    const top = await $.process.run(['git', 'rev-parse', '--show-toplevel']).then(
      r => (r.exitCode === 0 ? r.stdout.trim() : ''),
      () => ''
    )
    const script = top ? `${top}/${SCRIPT}` : SCRIPT
    const hasScript = await $.fs.stat(script).then(
      () => true,
      () => false
    )
    if (!hasScript) {
      await update($, snapshot, () => ({ progress: null, error: `в этой рабочей копии нет ${SCRIPT}`, at: Date.now() }))
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
        const r = await $.process.run(['node', script, '--json'], { timeoutMs: 20_000 })
        if (r.exitCode !== 0) {
          await update($, snapshot, () => ({ progress: null, error: r.stderr.trim().slice(0, 300) || `код ${r.exitCode}`, at: Date.now() }))
          return
        }
        const progress = JSON.parse(r.stdout) as Progress
        await update($, snapshot, () => ({ progress, error: '', at: Date.now() }))
        const running = progress.state === 'running'
        active = running || progress.state === 'hold' || progress.queue.length > 0
        $.ui.status(running || progress.state === 'hold' ? progress.line : undefined)
        if (running && !wasRunning) void $.ui.open({ id: PANE, title: TITLE })
        // прогон закончился (итог или удержание стенда): тост и уведомление с итогом
        if (wasRunning && !running && progress.state !== 'none') {
          const verdict =
            progress.state === 'stopped' ? 'оборван' : progress.verdict || 'закончен'
          const text = `Прогон ${progress.suite || ''}: ${verdict}`
          $.ui.toast(text)
          void $.ui.notify(text, { title: TITLE })
        }
        wasRunning = running
      } catch (err) {
        await update($, snapshot, () => ({ progress: null, error: String(err).slice(0, 300), at: Date.now() }))
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
    const { Box, Text } = $.ui.resolve(e)
    const snap = await read($, snapshot)
    const cols = (e.props as { bodyColumns?: number }).bodyColumns ?? e.viewport?.columns ?? 60
    const width = Math.max(10, Math.min(40, cols - 14))
    const p = snap.progress

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
      <Box flexDirection="column">
        {waiting.map(w => (
          <Text color="warning">{w}</Text>
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
    const red = /красный/.test(p.verdict)
    const stateText = running ? 'идёт' : p.state === 'stopped' ? 'оборван' : 'закончен'
    const until = new Date(p.holdUntil * 1000)
    const untilText = `${String(until.getHours()).padStart(2, '0')}:${String(until.getMinutes()).padStart(2, '0')}`
    const b = p.block
    const blockCounts = b
      ? Object.entries(b.counts)
          .filter(([, v]) => v)
          .map(([k, v]) => `${k} ${v}`)
          .join(' · ')
      : ''
    const fails = p.fails.slice(-FAILS_SHOWN)

    return (
      <Box flexDirection="column" gap={1}>
        {waitingBox}
        <Box flexDirection="column">
          <Text bold>
            {`${p.suite || 'прогон'} — ${stateText}`}
          </Text>
          <Text dimColor>{p.name}</Text>
          {!running && p.verdict && <Text color={red ? 'error' : 'success'}>{`Итог: ${p.verdict}`}</Text>}
        </Box>

        <Box flexDirection="column">
          <Text>
            {`прошло ${dur(p.elapsedSec)}${p.estSec ? ` · оценка ${dur(p.estSec)}` : ''}${
              running && p.overSec > 0
                ? ` · дольше оценки на ${dur(p.overSec)}`
                : running && p.etaSec != null
                  ? ` · осталось ~${dur(p.etaSec)}`
                  : ''
            }`}
          </Text>
          {p.percent != null && <Text mono>{`${bar(p.percent / 100, width)} ${p.percent}%`}</Text>}
          {p.session && <Text dimColor>{`Сессия: ${p.session}`}</Text>}
          {holding && (
            <Text color="suggestion">{`Стенд удержан для ручной сессии до ${untilText} (ещё ${dur(p.holdLeftSec)})`}</Text>
          )}
        </Box>

        {(running || holding) && (
          <Box flexDirection="column">
            {p.stages.map(st => (
              <Text>{`Этап: ${st.core !== '-' ? `${st.core} · ` : ''}${st.name} (${dur(st.sinceSec)})`}</Text>
            ))}
            {b && (
              <Text mono>{`${b.core} ${b.name}: ${bar(b.total ? b.done / b.total : 0, width)} ${b.done}/${b.total}${b.etaSec != null ? ` ~${dur(b.etaSec)}` : ''}`}</Text>
            )}
            {b && blockCounts && <Text dimColor>{blockCounts}</Text>}
            {b && b.current && (
              <Text dimColor>{`сейчас: ${b.current}${b.currentSec != null ? ` (${dur(b.currentSec)})` : ''}`}</Text>
            )}
            {running && p.idleSec > 300 && <Text color="warning">{`Нет новых событий ${dur(p.idleSec)}`}</Text>}
          </Box>
        )}

        <Text>{`${holding ? 'Ручная сессия' : 'Записано'}: PASS ${p.counts.PASS} · FAIL ${p.counts.FAIL} · KNOWN ${p.counts.KNOWN} · SKIP ${p.counts.SKIP}`}</Text>

        {fails.length > 0 && (
          <Box flexDirection="column">
            <Text color="error" bold>{`Упало (${p.fails.length})`}</Text>
            {fails.map(f => (
              <Text color={f.live ? 'warning' : 'error'}>{`✗ ${f.core !== '-' ? `[${f.core}] ` : ''}${f.name}`}</Text>
            ))}
            {p.fails.length > FAILS_SHOWN && <Text dimColor>{`… и ещё ${p.fails.length - FAILS_SHOWN}`}</Text>}
          </Box>
        )}
      </Box>
    )
  })
}
