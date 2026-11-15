// Контракт страницы «Прокси»: модуль подключается только страницей «Прокси»
// и components/proxies/*. Поля совпадают с routingContract.json и DTO Go.
import type { KernelCounts, ProbeConfig } from './configRouting';

export const PROXIES_BASE = '/api/configlayer/proxies';

export type ProxyKernel = 'xray' | 'mihomo';
export type RuntimeKernel = ProxyKernel | 'none' | '';
export type RuntimeMode = 'panel' | 'user_managed' | 'user_profile' | 'not_installed';
export type PingMethod = 'xray' | 'mihomo' | 'tls';
export type PingStatus = 'ok' | 'timeout' | 'error';

export interface RuntimeView {
  kernel: RuntimeKernel;
  running: ProxyKernel[];
  state: 'panel' | 'stopped' | 'user_profile' | 'user_managed' | 'conflict';
  modes: RuntimeModes;
  probe: ProbeConfig;
  groups: RuntimeGroup[];
}

export interface RuntimeModes {
  xray: RuntimeMode;
  mihomo: RuntimeMode;
}

export interface RuntimeGroup {
  id: string;
  name: string;
  mode: 'manual' | 'auto' | 'fallback' | 'source';
  current: string;
  current_label: string;
  chain: RuntimeLink[];
  fallback?: string;
  selectable: boolean;
  unconfirmed: boolean;
  locked_with: string[];
  counts: KernelCounts;
  members: RuntimeMember[];
}

export interface RuntimeLink {
  kind: 'group' | 'node' | 'direct' | 'reject';
  id?: string;
  ref?: string;
  name: string;
}

export interface RuntimeMember {
  member: string;
  kind: 'group' | 'node' | 'direct' | 'reject';
  ref?: string;
  group?: string;
  name: string;
  proto?: string;
  source?: string;
  xray: boolean;
  mihomo: boolean;
}

export interface RuntimeNow {
  kernel: RuntimeKernel;
  groups: Record<string, GroupNow>;
  history: Record<string, NowDelay>;
}

export interface GroupNow {
  ref?: string;
  known: boolean;
  fastest?: boolean;
}

export interface NowDelay {
  ms: number;
  at: string;
}

export interface PingSnapshot {
  batch?: PingBatch;
  results: Record<string, PingResult>;
}

export interface PingBatch {
  id: string;
  state: 'running' | 'done' | 'cancelled' | 'kernel_changed' | 'failed';
  scope: 'all' | 'group' | 'repick';
  group?: string;
  kernel: RuntimeKernel;
  method: PingMethod;
  total: number;
  done: number;
  excluded: number;
  fallback_reason?: 'no_xray_binary' | 'low_memory';
  reason_code?: string;
  started_at: string;
  finished_at?: string;
}

export interface PingResult {
  ms: number;
  status: PingStatus;
  reason?: string;
  at: string;
  kernel: RuntimeKernel;
  method: PingMethod;
  history: PingPoint[];
}

export interface PingPoint {
  ms: number;
  status: PingStatus;
  at: string;
}

export interface PingStart {
  batch_id: string;
  joined: boolean;
  batch: PingBatch;
}

export interface ConfirmResult {
  group: string;
  confirmed: boolean;
}
