// Канонические предикаты классификации групп/узлов прокси.
// Чистый модуль без обращений к DOM и localStorage — используется и в Proxies.svelte,
// и в unit-тестах напрямую.

export const LATENCY_FAST_MS = 300;
export const LATENCY_DEGRADED_MS = 800;

export type LatencyBucket = 'fast' | 'mid' | 'bad' | 'unchecked';

/**
 * Единая классификация задержки узла. Заменяет три расходящиеся копии этой
 * логики, ранее продублированные в computeStats() / getGroupHealthStats() /
 * getLatencyClass().
 */
export function classifyLatency(delay: number | undefined, alive: boolean): LatencyBucket {
  if (delay === undefined) return 'unchecked';
  if (!alive || delay === 0 || delay >= LATENCY_DEGRADED_MS) return 'bad';
  if (delay < LATENCY_FAST_MS) return 'fast';
  return 'mid';
}

export const SYSTEM_PROXY_NAMES = ['DIRECT', 'REJECT', 'REJECT-DROP', 'PASS', 'COMPATIBLE'];
export const SYSTEM_PROXY_TYPES = ['direct', 'reject', 'reject-drop', 'pass', 'compatible'];

/**
 * Единый предикат "это статический системный выход, а не реальный прокси-узел".
 * Заменяет три ad-hoc варианта exclusion-списков (computeStats/getGroupHealthStats/
 * getLatencyClass), которые ранее расходились в составе PASS/COMPATIBLE.
 */
export function isSystemProxy(name: string | undefined, type?: string): boolean {
  if (name && SYSTEM_PROXY_NAMES.includes(name.toUpperCase())) return true;
  if (type && SYSTEM_PROXY_TYPES.includes(type.toLowerCase())) return true;
  return false;
}

export const PROXY_GROUP_TYPES = ['selector', 'urltest', 'fallback', 'loadbalance', 'relay'];

export function isProxyGroupType(type: string | undefined): boolean {
  if (!type) return false;
  return PROXY_GROUP_TYPES.includes(type.toLowerCase());
}

export interface ClassifiableGroup {
  name: string;
  type: string;
  now: string;
  all: string[];
}

// Ключевые имена/паттерны, по которым группа считается корневым маршрутом (Core).
// GLOBAL — стабильное встроенное имя Mihomo; остальные паттерны — конвенция
// пресетов этого проекта (mihomoYaml.ts), не гарантированные фиксированные константы.
export const CORE_GROUP_PATTERNS: RegExp[] = [
  /^global$/i,
  /^proxy$/i,
  /^proxies$/i,
  /заблок/i,
  /blocked/i,
  /fallback/i,
  /^auto/i,
  /автовыбор/i
];

/**
 * true, если хотя бы одна ДРУГАЯ группа (не GLOBAL и не сама `name`) ссылается
 * на `name` в своём `all[]`. GLOBAL исключён из проверяющих групп намеренно:
 * Mihomo перечисляет в GLOBAL.all вообще всё, поэтому без исключения Core-ролью
 * пометились бы все группы разом.
 */
export function isReferencedByOtherGroup(name: string, groups: ClassifiableGroup[]): boolean {
  return groups.some((g) => g.name !== name && g.name !== 'GLOBAL' && g.all.includes(name));
}

export type GroupRole = 'core' | 'service' | 'system';

/**
 * Классификация роли группы: закрепление пользователем побеждает всё; затем
 * группа со сплошь системными узлами в all[] — служебная (system); затем
 * ключевое имя/топология — core; иначе — обычная сервисная группа.
 */
export function classifyGroupRole(
  group: ClassifiableGroup,
  groups: ClassifiableGroup[],
  pinnedNames: ReadonlySet<string>,
  proxyTypes: Record<string, string | undefined>
): GroupRole {
  if (pinnedNames.has(group.name)) return 'core';
  if (group.all.length > 0 && group.all.every((n) => isSystemProxy(n, proxyTypes[n]))) {
    return 'system';
  }
  if (CORE_GROUP_PATTERNS.some((re) => re.test(group.name))) return 'core';
  if (isReferencedByOtherGroup(group.name, groups)) return 'core';
  return 'service';
}

/**
 * Разбивает группы на три ведра (core/service/system), сохраняя исходный
 * порядок внутри каждого ведра.
 */
export function splitGroupsByRole<T extends ClassifiableGroup>(
  groups: T[],
  pinnedNames: ReadonlySet<string>,
  proxyTypes: Record<string, string | undefined>
): { core: T[]; service: T[]; system: T[] } {
  const core: T[] = [];
  const service: T[] = [];
  const system: T[] = [];
  for (const group of groups) {
    const role = classifyGroupRole(group, groups, pinnedNames, proxyTypes);
    if (role === 'core') core.push(group);
    else if (role === 'system') system.push(group);
    else service.push(group);
  }
  return { core, service, system };
}
