// Тексты ошибок ввода и ответа провайдера (D-31, D-32). Сервер отдаёт только код и
// параметры; заголовок («что случилось») и совет («что делать») берутся по коду из
// словарей: cfg.nodes.err.<код>.title и .hint. Полноту словарей по списку кодов
// проверяет scripts/check-config-ui.js (список — sourceErrorCodes.json).

import sourceErrorCodes from './sourceErrorCodes.json';
import type { SourceErrorPayload, VerdictView } from './configSources';

/** Функция перевода из стора $t. */
export type TFn = (key: string, params?: Record<string, string | number>) => string;

/** Все коды, которые знает сервер. */
export const SOURCE_ERROR_CODES: string[] = sourceErrorCodes;

const KNOWN = new Set(SOURCE_ERROR_CODES);

// Параметры, которые подставляются в тексты. Не пришедший параметр заменяется
// на «?», а не остаётся голым «{host}» в интерфейсе.
const PARAM_NAMES = [
  'status',
  'line',
  'key',
  'section',
  'scheme',
  'kernel_min',
  'date',
  'host',
  'type',
  'field',
  'mode',
  'network',
  'plugin',
  'limit',
  'unit'
];

function paramsOf(
  code: string,
  params: Record<string, string> | undefined,
  line?: number
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const name of PARAM_NAMES) out[name] = '?';
  if (line !== undefined && line > 0) out.line = String(line);
  for (const [k, v] of Object.entries(params ?? {})) {
    if (typeof v === 'string' && v !== '') out[k] = v;
  }
  out.code = code;
  return out;
}

/** Заголовок и совет по коду ошибки; неизвестный код — общий текст cfg.nodes.err.unknown. */
export function sourceErrorText(e: SourceErrorPayload, t: TFn): { title: string; hint: string } {
  const base = KNOWN.has(e.code) ? `cfg.nodes.err.${e.code}` : 'cfg.nodes.err.unknown';
  const p = paramsOf(e.code, e.params, e.line);
  return { title: t(`${base}.title`, p), hint: t(`${base}.hint`, p) };
}

/** Причина отказа ядра по вердикту матрицы возможностей; для годного узла — пустая строка. */
export function verdictReason(v: VerdictView, t: TFn): string {
  if (v.ok || !v.code) return '';
  return sourceErrorText({ code: v.code, params: v.params }, t).title;
}

/** Адреса в свободном тексте заменяются: в подробностях не должно быть полного URL (D-33). */
const URL_IN_TEXT = /[A-Za-z][A-Za-z0-9+.-]*:\/\/[^\s"'<>]+/g;

/** Фрагмент для показа: не длиннее max символов, обрезка помечается «…». */
export function truncateFragment(s: string, max = 80): string {
  const chars = Array.from(s);
  return chars.length <= max ? s : chars.slice(0, max).join('') + '…';
}

/** Строки блока «Подробности»: код, код HTTP, строка, фрагмент и описание сервера. */
export function errorDetailLines(e: SourceErrorPayload, t: TFn, file?: string): string[] {
  const lines: string[] = [t('cfg.nodes.detail_code', { code: e.code })];
  if (e.http_status) lines.push(t('cfg.nodes.detail_http', { status: e.http_status }));
  if (file) lines.push(t('cfg.nodes.detail_file', { file }));
  if (e.line) lines.push(t('cfg.nodes.detail_line', { line: e.line }));
  if (e.fragment) {
    lines.push(
      t('cfg.nodes.detail_fragment', {
        fragment: truncateFragment(e.fragment.replace(URL_IN_TEXT, '[url]'))
      })
    );
  }
  if (e.detail) {
    lines.push(
      t('cfg.nodes.detail_text', {
        detail: truncateFragment(e.detail.replace(URL_IN_TEXT, '[url]'), 200)
      })
    );
  }
  return lines;
}

/** Ошибка запроса к API (apiFetchJSON) → полезная нагрузка ошибки источника. */
export function errorFromThrown(err: unknown): SourceErrorPayload {
  const o = (typeof err === 'object' && err !== null ? err : {}) as {
    code?: unknown;
    params?: unknown;
    status?: unknown;
    message?: unknown;
  };
  const params =
    o.params && typeof o.params === 'object' ? (o.params as Record<string, string>) : undefined;
  const status = typeof o.status === 'number' && o.status > 0 ? o.status : undefined;
  return {
    code: typeof o.code === 'string' ? o.code : 'unknown_error',
    params,
    http_status: status
  };
}
