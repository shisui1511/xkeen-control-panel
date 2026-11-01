// Общий помощник форм секций ядер (D-07): чтение и запись поля по пути из FORM_KEYS,
// разбор проблем ключей ответа сервера, запись секции в черновик и таблица умолчаний ядер.
// Здесь нет рун Svelte: файл используют формы Xray (147-22) и Mihomo (147-23) и блок
// «Дополнительно». Решения о допустимости значений принимает сервер, помощник только
// раскладывает его ответ по полям.

import { get } from 'svelte/store';
import { t } from '../../../i18n';
import { saveSection, type CoreKernel, type KeyIssue } from '../../../lib/configCores';
import { applyRunning } from '../../../lib/configLayer';
import { coresBusy, guarded } from './coresOwner';

// --- Умолчания ядер ---

/**
 * Умолчания ядер для подсказки «по умолчанию: …»: ключ 'ядро.секция' → путь поля из
 * FORM_KEYS → значение, которое ядро берёт, если ключа нет. Источники: документация Xray
 * (log, dns, policy.levels) и Mihomo (общие настройки, dns, tun, sniffer). Поля без
 * умолчания (порты, списки серверов, имена) в таблице не перечислены: подсказки у них нет.
 */
export const KERNEL_DEFAULTS: Record<string, Record<string, string>> = {
  'xray.log': { loglevel: 'warning' },
  'xray.dns': { queryStrategy: 'UseIP', disableCache: 'false' },
  'xray.policy': {
    'levels.0.handshake': '4',
    'levels.0.connIdle': '300',
    'levels.0.uplinkOnly': '2',
    'levels.0.downlinkOnly': '5'
  },
  'mihomo.main': {
    mode: 'rule',
    'log-level': 'info',
    'allow-lan': 'false',
    ipv6: 'false',
    'bind-address': '*'
  },
  'mihomo.dns': { enable: 'false', 'enhanced-mode': 'normal', ipv6: 'false' },
  'mihomo.inbounds': {
    'tun.enable': 'false',
    'tun.stack': 'system',
    'tun.auto-route': 'false'
  },
  'mihomo.sniffer': {
    enable: 'false',
    'force-dns-mapping': 'false',
    'parse-pure-ip': 'false',
    'override-destination': 'false'
  }
};

// --- Путь поля ---

type Obj = Record<string, unknown>;

function isObj(v: unknown): v is Obj {
  return v !== null && typeof v === 'object' && !Array.isArray(v);
}

/** Сегменты пути: «a.b.0.c» → [a, b, 0, c]; пустые сегменты отбрасываются. */
function segments(path: string): string[] {
  return path.split('.').filter((s) => s !== '');
}

/**
 * Значение поля по пути. Вложенные пути идут через точку (levels.0.handshake: «0» — ключ
 * объекта). Путь, в котором есть «[]» (строки списков: «[].tag», «listeners[].name»),
 * возвращает массив значений поля по элементам списка. Нет значения — undefined.
 */
export function getPath(obj: unknown, path: string): unknown {
  const walk = (cur: unknown, segs: string[]): unknown => {
    if (segs.length === 0) return cur;
    const [head, ...rest] = segs;
    if (head === '[]' || head.endsWith('[]')) {
      const key = head === '[]' ? null : head.slice(0, -2);
      const list = key === null ? cur : isObj(cur) ? cur[key] : undefined;
      if (!Array.isArray(list)) return undefined;
      return list.map((item) => walk(item, rest));
    }
    if (!isObj(cur)) return undefined;
    return walk(cur[head], rest);
  };
  return walk(obj, segments(path));
}

function isEmpty(value: unknown): boolean {
  return value === '' || value === null || value === undefined;
}

/**
 * Новый объект с полем по пути. Пустая строка, null и undefined снимают ключ, а опустевшие
 * родительские объекты убираются: секция не обрастает ключами, которых в ней не было.
 * Исходный объект не меняется. Пути со списками («[]») не пишутся: элементы списка форма
 * собирает сама и записывает массив целиком по ключу списка.
 */
export function setPath(obj: unknown, path: string, value: unknown): Obj {
  const segs = segments(path);
  const base: Obj = isObj(obj) ? (JSON.parse(JSON.stringify(obj)) as Obj) : {};
  if (segs.length === 0 || segs.some((s) => s.endsWith('[]'))) return base;

  const chain: Obj[] = [base];
  let cur = base;
  for (const key of segs.slice(0, -1)) {
    const next = cur[key];
    if (isObj(next)) {
      cur = next;
    } else if (isEmpty(value)) {
      return base;
    } else {
      const created: Obj = {};
      cur[key] = created;
      cur = created;
    }
    chain.push(cur);
  }

  const last = segs[segs.length - 1];
  if (isEmpty(value)) {
    delete cur[last];
    // Опустевшие родители убираются снизу вверх
    for (let i = chain.length - 1; i > 0; i--) {
      if (Object.keys(chain[i]).length > 0) break;
      delete chain[i - 1][segs[i - 1]];
    }
  } else {
    cur[last] = value;
  }
  return base;
}

/** Идентификатор поля для тестов: section-field-{ядро}-{секция}-{путь без знаков}. */
export function fieldTestId(kernel: CoreKernel, id: string, path: string): string {
  const slug = path
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
  return `section-field-${kernel}-${id}-${slug}`;
}

// --- Проблемы ключей ---

/** Коды ошибок записи секции, которые показываются у поля или блока «Дополнительно». */
const FIELD_ISSUE_CODES = new Set([
  'raw_reserved',
  'raw_pinned',
  'raw_dup_key',
  'raw_syntax',
  'controller_no_secret',
  'root_key_duplicate',
  'inbound_port_invalid',
  'inbound_duplicate'
]);

/** Проблемы по ключу поля: на один ключ — первая проблема. */
export function issuesByField(issues: KeyIssue[]): Map<string, KeyIssue> {
  const map = new Map<string, KeyIssue>();
  for (const issue of issues) {
    if (!map.has(issue.key)) map.set(issue.key, issue);
  }
  return map;
}

/** Проблема ключа из ошибки записи; null, если ошибка другого рода (разбирает тост). */
function issueOfError(err: unknown): KeyIssue | null {
  const e = err as { code?: unknown; params?: unknown } | null;
  if (typeof e?.code !== 'string' || !FIELD_ISSUE_CODES.has(e.code)) return null;
  const params =
    e.params && typeof e.params === 'object' ? (e.params as Record<string, unknown>) : {};
  const key = typeof params.key === 'string' ? params.key : '';
  return { key, code: e.code };
}

interface QueuedWrite {
  kernel: CoreKernel;
  id: string;
  form: unknown;
  advanced: string;
  resolve: (issues: KeyIssue[]) => void;
}

/** Записи секций, ждущие своей очереди: по одной, последней, на секцию. */
const queued = new Map<string, QueuedWrite>();
let draining = false;

/** Одна запись секции под признаком «идёт запрос»; attempted — запрос действительно ушёл. */
async function writeSection(job: QueuedWrite): Promise<{ attempted: boolean; issues: KeyIssue[] }> {
  const issues: KeyIssue[] = [];
  let attempted = false;
  await guarded(async () => {
    attempted = true;
    try {
      await saveSection(job.kernel, job.id, job.form, job.advanced);
    } catch (err) {
      const issue = issueOfError(err);
      if (issue === null) throw err;
      issues.push(issue);
    }
  });
  return { attempted, issues };
}

/** Выполняет ожидающие записи по одной, когда не идёт другой запрос и «Применить». */
async function drain(): Promise<void> {
  if (draining) return;
  draining = true;
  try {
    while (!get(coresBusy) && !get(applyRunning)) {
      const first = queued.entries().next();
      if (first.done) return;
      const [key, job] = first.value;
      queued.delete(key);
      const { attempted, issues } = await writeSection(job);
      if (!attempted) {
        // запрос перехватил другой: запись возвращается в очередь, если нет более новой
        if (queued.has(key)) job.resolve([]);
        else queued.set(key, job);
        return;
      }
      job.resolve(issues);
    }
  } finally {
    draining = false;
  }
}

// Очередь разбирается, как только занятость снята
coresBusy.subscribe((busy) => {
  if (!busy) void drain();
});
applyRunning.subscribe((running) => {
  if (!running) void drain();
});

/**
 * Записывает значения формы и текст «Дополнительно» секции в черновик. Выполняется под
 * признаком «идёт запрос» (guarded). Если в этот момент идёт другая запись или «Применить»,
 * запись не теряется, а ждёт очереди: на секцию остаётся последняя, прежняя ожидающая
 * считается заменённой (пустой список). Отказ по ключу (панельный, закреплённый, повтор,
 * secret контроллера, порт и дубль входящих) возвращается списком проблем, остальные
 * ошибки разбирает guarded (тост, перечитывание слоя). Пустой список — записано.
 */
export function commitSection(
  kernel: CoreKernel,
  id: string,
  form: unknown,
  advanced: string
): Promise<KeyIssue[]> {
  const key = `${kernel}.${id}`;
  return new Promise((resolve) => {
    queued.get(key)?.resolve([]);
    queued.delete(key);
    queued.set(key, { kernel, id, form, advanced, resolve });
    void drain();
  });
}

// --- Проверка чисел ---

/**
 * Проверка целого поля формы: пустое значение допустимо (ключ не задан), иначе целое
 * от min до max. Возвращает текст ошибки cfg.sec.err_int либо пустую строку, если верно.
 */
export function intField(
  value: string | number | null | undefined,
  min: number,
  max: number
): string {
  const text = value === null || value === undefined ? '' : String(value).trim();
  if (text === '') return '';
  const ok = /^-?\d{1,9}$/.test(text) && Number(text) >= min && Number(text) <= max;
  return ok ? '' : get(t)('cfg.sec.err_int', { min, max });
}
