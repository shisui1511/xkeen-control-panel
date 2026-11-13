export type Fail = { target?: string; core: string; name: string; detail: string; live: boolean }
export type Stage = { target: string; core: string; name: string; sinceSec: number }
export type Block = {
  kind: 'go' | 'pw'
  target: string
  core: string
  name: string
  done: number
  total: number
  counts: Record<string, number>
  current: string
  currentSec?: number | null
  etaSec?: number | null
}
export type Progress = {
  name: string
  suite: string
  state: 'none' | 'running' | 'hold' | 'done' | 'stopped'
  verdict: string
  elapsedSec: number
  estSec: number | null
  etaSec: number | null
  percent: number | null
  /** Живой прогноз полной длительности (сессия с оценками тестов); null — только оценка. */
  forecastSec?: number | null
  stages: Stage[]
  /** Идущие запуски: по одному на цель (цели идут параллельно). */
  blocks: Block[]
  counts: { PASS: number; FAIL: number; KNOWN: number; SKIP: number }
  fails: Fail[]
  session: string
  idleSec: number
  holdUntil: number
  holdLeftSec: number
  overSec: number
  queue: { suite: string; sinceSec: number }[]
  unattended: { at: string } | null
  line: string
}
export type Snapshot = { progress: Progress | null; error: string; at: number }

declare module 'claude-code' {
  interface PluginState {
    'router-watch': { snapshot: Shaped<Snapshot> }
  }
}
