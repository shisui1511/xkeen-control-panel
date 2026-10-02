import { writable, derived, get } from 'svelte/store';
import { apiFetchJSON } from './lib/api';
import { isServiceRestarting, clearRestartGrace } from './lib/serviceGrace';
import { NAV_CAPS_KEY, parseNavCaps, toNavCaps, mergeNavCaps, type NavCaps } from './lib/navCaps';
import { kernelStateOf, kernelNameOf, runningKernelsOf } from './lib/kernelState';

// --- Capabilities store ---

export interface KernelCapability {
  installed: boolean;
  version?: string;
  channel?: string;
}

export interface CapabilitiesData {
  kernels: Record<string, KernelCapability>;
  /** xray | mihomo | none | both (оба ядра запущены — конфликт). */
  active_kernel: string;
  /** Запущены оба ядра; интерфейс читает его через isConflict. */
  kernel_conflict?: boolean;
  /** Запущенные ядра в порядке xray, mihomo. */
  running_kernels?: string[];
  xkeen_dns?: boolean;
  xkeen_installed?: boolean;
  mihomo: {
    reachable: boolean;
    process_running: boolean;
    api_reachable: boolean;
    api_authenticated: boolean;
    api_url?: string;
    discovered_secret?: string;
    controller_type?: string;
    controller_target?: string;
    is_insecure_lan?: boolean;
  };
  xray?: {
    conf_dir: string;
    conf_dir_exists: boolean;
    grpc_ready?: boolean;
    /** Адрес gRPC API Xray, которым пользуется панель (127.0.0.1:<порт>). */
    api_addr?: string;
  };
  global_hwid?: string;
  /** Prediction of "will Apply restart this kernel" per kernel (button labels). */
  apply_restarts?: Record<string, boolean>;
}

export const capabilities = writable<CapabilitiesData | null>(null);
export const isKernelChecking = writable(false);

// --- Единое состояние активного ядра (D-07) ---
// Единственный источник «какое ядро активно / конфликт». Компоненты не читают
// $capabilities.active_kernel напрямую. До ответа capabilities все булевы false,
// kernelKnown false (страницы показывают скелетон, а не заглушку «ядро не запущено»).
export const activeKernelState = derived(capabilities, kernelStateOf);
export const kernelKnown = derived(capabilities, (c) => c !== null);
export const isXray = derived(activeKernelState, (s) => s === 'xray');
export const isMihomo = derived(activeKernelState, (s) => s === 'mihomo');
export const isNone = derived(activeKernelState, (s) => s === 'none');
export const isConflict = derived(activeKernelState, (s) => s === 'conflict');
/** Имя единственного активного ядра; при конфликте и none — пустая строка. */
export const activeKernelName = derived(activeKernelState, kernelNameOf);
export const runningKernels = derived(capabilities, runningKernelsOf);
/** Конфликт виден UI: не во время окна перезапуска (переходные процессы не конфликт). */
export const conflictVisible = derived([isConflict, isServiceRestarting], ([c, r]) => c && !r);

// --- Состояние API Mihomo (tri-state) ---
// unknown — ответа capabilities ещё не было (опросы не идут, оффлайн не показываем);
// up / down — по api_reachable последнего ответа. fetchCapabilities обновляет его на
// каждом такте (10 с). Общий гейт опросов: usePoller(..., { enabledWhen: mihomoApiReady }).
export type MihomoApiState = 'unknown' | 'up' | 'down';
export const mihomoApiState = writable<MihomoApiState>('unknown');
// В конфликте живые маршруты Mihomo отвечают 409 — опросы прокси и WebSocket трафика
// не идут (гейт закрыт), даже если api_reachable истинен.
export const mihomoApiReady = derived(
  [mihomoApiState, activeKernelState],
  ([s, k]) => s === 'up' && k !== 'conflict'
);
// Совместимость с Sidebar и быстрым стартом дашборда: прежний boolean по смыслу «up».
export const mihomoApiAvailable = derived(mihomoApiState, (s) => s === 'up');
// Причина «оффлайн» для подписей: null пока API работает или состояние неизвестно.
export type MihomoOfflineReason = 'not_running' | 'api_down' | null;
export const mihomoOfflineReason = derived(
  [mihomoApiState, capabilities],
  ([$state, $caps]): MihomoOfflineReason => {
    if ($state !== 'down') return null;
    return $caps?.mihomo?.process_running ? 'api_down' : 'not_running';
  }
);

// --- Срез capabilities для бокового меню ---
// Последний известный срез хранится в localStorage: первый кадр меню рисуется
// по нему, а не по null, и после ответа сервера группы не мигают (D-16).
// Хранится только toNavCaps-срез: без discovered_secret и global_hwid.
export function readNavCaps(): NavCaps | null {
  try {
    return parseNavCaps(localStorage.getItem(NAV_CAPS_KEY));
  } catch {
    // localStorage недоступен — меню работает как без кэша
    return null;
  }
}

function writeNavCaps(v: NavCaps): void {
  try {
    localStorage.setItem(NAV_CAPS_KEY, JSON.stringify(v));
  } catch {
    // localStorage может быть недоступен или переполнен
  }
}

export const navCaps = writable<NavCaps | null>(readNavCaps());

// Безопасный срез «ядро не определилось»: группы Mihomo не рисуются. Ставится, когда
// кэша нет, а сервер не дал пригодного ответа; в localStorage не записывается.
const SAFE_NAV_CAPS: NavCaps = {
  v: 1,
  active_kernel: 'none',
  kernels: { xray: false, mihomo: false }
};

// Число взятых замков меню. Пока оно больше нуля, ответы capabilities не меняют
// navCaps (стор capabilities обновляется как обычно): во время установки пункты
// меню не скрываются и не появляются скачком (D-16).
export const navLockCount = writable(0);

/**
 * Замораживает обновление бокового меню. Брать на время установки XKeen или
 * ядра, снимать в finally или при размонтировании компонента. Возвращает
 * идемпотентную функцию снятия; после снятия последнего замка выполняется
 * одно тихое обновление capabilities.
 */
export function lockNav(): () => void {
  navLockCount.update((n) => n + 1);
  let released = false;
  return () => {
    if (released) return;
    released = true;
    navLockCount.update((n) => Math.max(0, n - 1));
    if (get(navLockCount) === 0) void fetchCapabilities();
  };
}

let lastValidActiveKernel = '';
let consecutiveCapabilitiesFailures = 0;

export async function fetchCapabilities(signal?: AbortSignal): Promise<void> {
  try {
    const data = await apiFetchJSON<CapabilitiesData>('/api/capabilities', { signal });

    consecutiveCapabilitiesFailures = 0;
    if (get(isServiceRestarting) && data.mihomo?.api_reachable) {
      clearRestartGrace();
    }

    // «Последнее валидное» работает только для переходного none (или пустого поля):
    // известное ядро не затирается. Конфликт ничем не маскируется и не запоминается —
    // он должен быть виден немедленно (D-07).
    const incomingState = kernelStateOf(data);
    if (incomingState === 'xray' || incomingState === 'mihomo') {
      lastValidActiveKernel = incomingState;
    } else if (incomingState === 'none' && lastValidActiveKernel) {
      data.active_kernel = lastValidActiveKernel;
    }

    if (get(isKernelChecking)) {
      capabilities.update((current) => {
        // Текущее значение сохраняется, только если ни старый, ни новый ответ не конфликтные
        if (current && kernelStateOf(current) !== 'conflict' && incomingState !== 'conflict') {
          return {
            ...data,
            active_kernel: current.active_kernel
          };
        }
        return data;
      });
    } else {
      capabilities.set(data);
    }

    // Меню заморожено на время установки: срез применится после снятия замка
    if (get(navLockCount) === 0) {
      // Конфликт в кэш меню не пишется: безопасный дефолт после сбоев (136-18)
      const next = incomingState === 'conflict' ? null : toNavCaps(data);
      if (next) {
        const merged = mergeNavCaps(get(navCaps), next);
        navCaps.set(merged);
        writeNavCaps(merged);
      } else if (get(navCaps) === null) {
        // Ответ без пригодного активного ядра и кэша нет: скелетон не должен
        // висеть вечно. Как «ядро не определилось» — группы Mihomo, без записи в кэш.
        navCaps.set(SAFE_NAV_CAPS);
      }
    }

    // Состояние API Mihomo обновляется на каждом успешном ответе. Опросы прокси ядра,
    // Sidebar и быстрый старт дашборда подписаны на него реактивно (D-08, D-09).
    mihomoApiState.set(data.mihomo?.api_reachable ? 'up' : 'down');
  } catch (e: any) {
    // Cancelled poll: no state change.
    if (e?.name === 'AbortError') return;
    // Session expired: apiFetch already handled logout+toast+redirect centrally,
    // wiping capabilities state here would just flash stale UI before the
    // navigation completes.
    if (e?.status === 401) return;

    consecutiveCapabilitiesFailures++;
    // Debounce network blips: during active service restart or for a single intermittent failure,
    // do not instantly set mihomoApiState to down to avoid UI flickering.
    if (!get(isServiceRestarting) && consecutiveCapabilitiesFailures >= 2) {
      mihomoApiState.set('down');
    }

    // Холодный вход и API недоступен: без кэша скелетон меню висел бы вечно.
    // После двух сбоев подряд (тот же порог дребезга) показываем безопасный дефолт.
    // В localStorage он не пишется: кэш хранит только срез, подтверждённый
    // сервером (D-16), а первый успешный ответ заменит дефолт настоящим срезом.
    if (get(navCaps) === null && consecutiveCapabilitiesFailures >= 2 && get(navLockCount) === 0) {
      navCaps.set(SAFE_NAV_CAPS);
    }
  }
}

// UI state: controls whether the off-canvas sidebar is visible on mobile
export const isSidebarOpen = writable(false);

// UI state: desktop icon-rail sidebar collapse (persistent, NOT the mobile
// off-canvas drawer above — isSidebarOpen and isSidebarCollapsed are separate
// mechanisms and must not be merged, D-12/D-13/D-14).
function readInitialCollapsed(): boolean {
  try {
    const saved = localStorage.getItem('sidebar_collapsed');
    return saved === 'true';
  } catch (e) {
    // localStorage unavailable or corrupted — fail-closed to expanded (false)
    return false;
  }
}

export const isSidebarCollapsed = writable<boolean>(readInitialCollapsed());

isSidebarCollapsed.subscribe((v) => {
  try {
    localStorage.setItem('sidebar_collapsed', String(v));
  } catch (e) {
    // localStorage may be unavailable
  }
});

// --- Toast store ---

export interface ToastAction {
  label: string;
  onClick: () => void;
}

export interface ToastItem {
  id: number;
  type: 'success' | 'error' | 'info' | 'warning';
  message: string;
  duration?: number;
  action?: ToastAction;
}

export const toastStore = writable<ToastItem[]>([]);

// --- Panel reachability store (D-20) ---
// Set by lib/panelHealth.ts's reportPanelUnreachable() whenever apiFetch's
// underlying fetch() throws (network down — xcp restarting/updating/being
// deployed). While true, showToast() suppresses new error toasts so a flood
// of failed background polls does not spam the user; the reconnect banner
// in Dashboard.svelte is the single non-blocking indicator instead.
export const panelUnreachable = writable<boolean>(false);

let _toastCounter = 0;

export function showToast(
  type: ToastItem['type'],
  message: string,
  duration = 4000,
  action?: ToastAction
): void {
  if (type === 'error' && get(panelUnreachable)) return;
  const id = ++_toastCounter;
  toastStore.update((items) => [...items, { id, type, message, duration, action }]);
  if (duration > 0) {
    setTimeout(() => {
      toastStore.update((items) => items.filter((t) => t.id !== id));
    }, duration);
  }
}

// --- ConfirmDialog store ---

export interface ConfirmRequest {
  title: string;
  message?: string;
  objectName?: string;
  consequence?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: 'danger' | 'warning' | 'primary';
  resolve: (value: boolean) => void;
}

export const confirmStore = writable<ConfirmRequest | null>(null);

export function showConfirm(
  titleOrOptions: string | Omit<ConfirmRequest, 'resolve'>,
  message?: string,
  confirmLabel?: string,
  cancelLabel?: string
): Promise<boolean> {
  return new Promise((resolve) => {
    if (typeof titleOrOptions === 'object' && titleOrOptions !== null) {
      confirmStore.set({
        variant: 'danger',
        ...titleOrOptions,
        resolve
      });
    } else {
      confirmStore.set({
        title: String(titleOrOptions),
        message,
        confirmLabel,
        cancelLabel,
        variant: 'danger',
        resolve
      });
    }
  });
}

// --- Dev mode store ---

export const devMode = writable(false);

/**
 * A file another page asks the Editor to open (absolute path). The Editor
 * consumes and clears it on mount.
 */
export const editorOpenRequest = writable<string | null>(null);

export async function fetchDevMode(): Promise<void> {
  try {
    const data = await apiFetchJSON<{ dev_mode: boolean }>('/api/settings');
    devMode.set(data.dev_mode ?? false);
  } catch (_) {
    // ignore
  }
}

export async function setDevMode(enabled: boolean): Promise<void> {
  try {
    await apiFetchJSON('/api/settings/dev-mode', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled })
    });
    devMode.set(enabled);
  } catch (e) {
    devMode.set(!enabled);
    showToast('error', e instanceof Error ? e.message : String(e));
  }
}

// --- Theme density store (DENS-01) ---

export type ThemeDensity = 'auto' | 'comfortable' | 'compact';

export function resolveDensityAttr(mode?: ThemeDensity): 'comfortable' | 'compact' {
  let densityMode = mode;
  if (!densityMode) {
    try {
      const saved = localStorage.getItem('theme_density');
      if (saved === 'comfortable' || saved === 'compact') {
        densityMode = saved;
      }
    } catch (_) {}
  }
  if (densityMode === 'compact' || densityMode === 'comfortable') {
    return densityMode;
  }
  const isMobile =
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(max-width: 1024px)').matches;
  return isMobile ? 'compact' : 'comfortable';
}

function readInitialDensity(): ThemeDensity {
  try {
    const saved = localStorage.getItem('theme_density');
    if (saved === 'comfortable' || saved === 'compact') {
      return saved;
    }
  } catch (e) {
    // localStorage unavailable
  }
  return 'auto';
}

export const themeDensity = writable<ThemeDensity>(readInitialDensity());

export function applyDensity(mode: ThemeDensity): void {
  try {
    if (mode === 'auto') {
      localStorage.removeItem('theme_density');
    } else {
      localStorage.setItem('theme_density', mode);
    }
    if (typeof document !== 'undefined') {
      document.documentElement.setAttribute('data-density', resolveDensityAttr(mode));
    }
  } catch (e) {
    // localStorage or DOM unavailable
  }
  themeDensity.set(mode);
}

let densityMql: MediaQueryList | null = null;
let handleDensityMediaChange: ((e: MediaQueryListEvent) => void) | null = null;

export function initDensity(): () => void {
  if (typeof document !== 'undefined') {
    document.documentElement.setAttribute('data-density', resolveDensityAttr());
  }
  if (typeof window !== 'undefined' && typeof window.matchMedia === 'function' && !densityMql) {
    densityMql = window.matchMedia('(max-width: 1024px)');
    handleDensityMediaChange = (e: MediaQueryListEvent) => {
      if (get(themeDensity) === 'auto' && typeof document !== 'undefined') {
        document.documentElement.setAttribute(
          'data-density',
          e.matches ? 'compact' : 'comfortable'
        );
      }
    };
    densityMql.addEventListener('change', handleDensityMediaChange);
  }
  return () => {
    if (densityMql && handleDensityMediaChange) {
      densityMql.removeEventListener('change', handleDensityMediaChange);
      densityMql = null;
      handleDensityMediaChange = null;
    }
  };
}
