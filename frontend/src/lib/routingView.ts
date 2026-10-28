// Помощники представления вкладок «Группы» и «Правила»: ключи словарей и подписи по
// данным сервера. Ничего не считают сами — состав группы, ссылки и ошибки приходят
// от бэкенда готовыми; здесь только выбор текста и вида чипа. Выбор ядра (Xray или
// Mihomo) общий с вкладкой «Узлы» — nodesView.chosenKernel.

import { writable } from 'svelte/store';
import type { GroupView, IssueView, KernelCounts, Member, Target } from './configRouting';
import type { TFn } from './sourceErrors';

// Состояние вкладок живёт на уровне модуля, а не компонента: вкладки раздела
// «Конфигурация» размонтируются при смене, а раскрытые карточки должны её пережить.

/** Раскрытые карточки групп по идентификатору. */
export const expandedGroups = writable<Record<string, boolean>>({});

export function toggleGroupExpanded(id: string): void {
  expandedGroups.update((m) => ({ ...m, [id]: !m[id] }));
}

/** Типы условий, у которых есть подпись cfg.rules.cond_<тип>. */
const COND_TYPES = new Set([
  'domain',
  'geosite',
  'ip',
  'geoip',
  'port',
  'network',
  'src_ip',
  'bittorrent'
]);

/** Ключ короткой подписи условия в чипе строки правила («домен», «порт»…). */
export function condLabelKey(type: string): string {
  return COND_TYPES.has(type) ? `cfg.rules.cond_${type}` : 'cfg.rules.cond_domain';
}

/** Ключ названия типа условия в выпадающем списке диалога («Домены», «Список доменов (geosite)»…). */
export function condTypeKey(type: string): string {
  return COND_TYPES.has(type) ? `cfg.rules.type_${type}` : 'cfg.rules.type_domain';
}

/**
 * Поле правила Xray, в которое попадает условие. То же сопоставление, что таблица
 * xrayCondFields в internal/services/routing/validate.go и xrayRule в rules_xray.go; их
 * сверяет scripts/check-routing-api.js. В правиле — одно условие на поле (cond_conflict):
 * Xray не пересекает два списка одного поля, «или» делается вторым правилом.
 */
export const COND_XRAY_FIELD: Record<string, string> = {
  domain: 'domain',
  geosite: 'domain',
  ip: 'ip',
  geoip: 'ip',
  port: 'port',
  network: 'network',
  src_ip: 'source',
  bittorrent: 'protocol'
};

/** Поле Xray условия по типу; неизвестный тип — сам тип. */
export function condXrayField(type: string): string {
  return COND_XRAY_FIELD[type] ?? type;
}

/** Подпись цели правила или участника: имя группы, DIRECT или REJECT. */
export function targetLabel(
  t: TFn,
  target: Target | Member,
  groups: ReadonlyArray<{ id: string; name: string }>
): string {
  void t;
  if (target.kind === 'direct') return 'DIRECT';
  if (target.kind === 'reject') return 'REJECT';
  const g = groups.find((x) => x.id === target.id);
  return g ? g.name : '?';
}

/** Число вложенных групп среди участников ручной группы. */
export function nestedCount(group: Pick<GroupView, 'members'>): number {
  return group.members.filter((m) => m.kind === 'group').length;
}

/**
 * Сводка состава по ядрам: одно установленное ядро — «Узлов: {n}», два — «Xray: {x} · Mihomo: {m}»;
 * вложенные группы добавляют «· вложено: {k}». Число null (ядро не установлено) считается нулём.
 */
export function kernelSummary(
  t: TFn,
  counts: KernelCounts,
  installed: Record<string, boolean>,
  nested = 0
): string {
  const x = counts.xray ?? 0;
  const m = counts.mihomo ?? 0;
  let text: string;
  if (installed.xray && installed.mihomo) {
    text = t('cfg.groups.summary_both', { x, m });
  } else if (installed.xray) {
    text = t('cfg.groups.summary_nodes', { n: x });
  } else if (installed.mihomo) {
    text = t('cfg.groups.summary_nodes', { n: m });
  } else {
    text = '';
  }
  if (nested > 0) {
    const extra = t('cfg.groups.summary_nested', { k: nested });
    text = text ? `${text} · ${extra}` : extra;
  }
  return text;
}

/** Известные коды проблем и предупреждений проверки (IssueView.code). */
const ISSUE_CODES = new Set([
  'group_empty',
  'group_cycle',
  'target_missing',
  'section_invalid',
  'list_not_found',
  'list_too_large',
  'cond_conflict',
  'geodata_ref_remains'
]);

/**
 * Ключ текста проблемы блокирующей проверки по коду: cfg.hint.<код>
 * (тот же словарь, что у хода применения); незнакомый код — общий текст сбоя проверки.
 */
export function issueTextKey(code: string): string {
  return ISSUE_CODES.has(code) ? `cfg.hint.${code}` : 'cfg.hint.checker_failed';
}

/** Вид чипа проблемы: красный блокирует «Применить», жёлтый — предупреждение. */
export type ChipKind = 'danger' | 'warning';

const DANGER_CODES = new Set([...ISSUE_CODES]);

/** Вид чипа по проблеме (issues — красный) или предупреждению (warnings — жёлтый). */
export function chipKind(issue: Pick<IssueView, 'code'>, fromIssues = true): ChipKind {
  return fromIssues && DANGER_CODES.has(issue.code) ? 'danger' : 'warning';
}
