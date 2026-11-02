import { get } from 'svelte/store';
import { t } from '../i18n';

// Тексты ошибок в тостах — человеческим языком. Бэкенд и fetch отдают то JSON
// целиком ({"success":false,"error":"…"}), то технические фразы на английском
// («path traversal detected», «connection refused»). Здесь из JSON достаётся
// само сообщение, а известные фразы заменяются понятным текстом: что случилось
// и что сделать.

// Подстрока сырого сообщения → ключ i18n. Порядок важен: первое совпадение.
const KNOWN: [RegExp, string][] = [
  [/path traversal|path not allowed/i, 'errors.path_not_allowed'],
  [/too many login attempts|account locked/i, 'errors.login_locked'],
  [/session not found|unauthorized|\bHTTP 401\b/i, 'errors.session_expired'],
  [
    /failed to fetch|networkerror|load failed|connection refused|ECONNREFUSED/i,
    'errors.router_unreachable'
  ],
  [/deadline exceeded|timed? ?out|таймаут/i, 'errors.timeout'],
  [/permission denied|operation not permitted/i, 'errors.permission_denied'],
  [/no such file or directory/i, 'errors.file_not_found'],
  [/no space left on device/i, 'errors.no_space']
];

/** Достаёт сообщение из JSON-ответа API, если строка его содержит. */
export function extractApiMessage(raw: string): string {
  const start = raw.indexOf('{');
  const end = raw.lastIndexOf('}');
  if (start === -1 || end <= start) return raw;
  try {
    const obj = JSON.parse(raw.slice(start, end + 1)) as Record<string, unknown>;
    const msg = [obj.error, obj.message, obj.detail].find(
      (v): v is string => typeof v === 'string' && v.trim() !== ''
    );
    if (!msg) return raw;
    const prefix = raw.slice(0, start).trim();
    return prefix ? `${prefix} ${msg}` : msg;
  } catch {
    return raw;
  }
}

/** Ключ i18n для известной технической фразы или null. */
export function knownErrorKey(raw: string): string | null {
  for (const [re, key] of KNOWN) if (re.test(raw)) return key;
  return null;
}

/**
 * Готовит текст ошибки для показа: JSON → сообщение, известная фраза →
 * понятный текст (префикс вида «Ошибка загрузки конфига:» сохраняется),
 * «HTTP 5xx» → «ошибка на роутере».
 */
export function humanizeError(
  raw: string,
  tr: (key: string, vars?: Record<string, string | number>) => string = get(t)
): string {
  if (!raw) return raw;
  const msg = extractApiMessage(raw).replace(/^Error:\s*/i, '');
  const key = knownErrorKey(msg);
  if (key) {
    // «Ошибка загрузки конфига: path traversal…» → «Ошибка загрузки конфига: <понятный текст>»
    const colon = msg.indexOf(':');
    const prefix =
      colon > 0 && !KNOWN.some(([re]) => re.test(msg.slice(0, colon)))
        ? msg.slice(0, colon + 1)
        : '';
    const text = tr(key);
    return prefix ? `${prefix} ${text}` : text.charAt(0).toUpperCase() + text.slice(1);
  }
  const http = msg.match(/\bHTTP (5\d\d)\b/);
  if (http) return msg.replace(http[0], tr('errors.server_error', { code: http[1] }));
  return msg;
}
