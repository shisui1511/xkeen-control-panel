/**
 * xrayMerge.ts — Read-Modify-Write merge для файлов конфигурации Xray.
 *
 * Стратегия: обновлять только управляемые ключи в каждом файле,
 * сохраняя все остальные ключи без изменений (D-04).
 *
 * Stub для Plan 15.2-01 (тестовая инфраструктура).
 * Реализация — Plan 15.2-02/04.
 */

export interface XrayMergeOptions {
  /** Имя файла (e.g. '02_dns.json') — определяет стратегию merge */
  fileName: string;
  /** Существующее содержимое файла (прочитанное через GET /api/config/read) */
  existing: Record<string, unknown>;
  /** Управляемые ключи из конструктора */
  managed: Record<string, unknown>;
}

/**
 * mergeXrayFile — точечное обновление управляемых ключей в Xray JSON-файле.
 *
 * Сохраняет все неуправляемые ключи (D-04).
 * Идемпотентен: повторный вызов с теми же данными возвращает идентичный результат (D-11).
 *
 * @param fileName - имя файла (01_log.json, 02_dns.json, ...)
 * @param existing  - текущее содержимое файла
 * @param managed   - управляемые данные из конструктора
 * @returns merged  - объединённый объект для записи
 */
export function mergeXrayFile(
  fileName: string,
  existing: Record<string, any>,
  managed: Record<string, any>
): Record<string, any> {
  switch (fileName) {
    case '01_log.json':
      return {
        ...existing,
        log: {
          ...(existing.log as Record<string, any>),
          loglevel: managed.loglevel,
          dnsLog: managed.dnsLog
        }
      };
    case '02_dns.json':
      return {
        ...existing,
        dns: {
          ...(existing.dns as Record<string, any>),
          servers: managed.servers,
          queryStrategy: managed.queryStrategy,
          hosts: managed.hosts
        }
      };
    case '03_inbounds.json': {
      const existingInbounds = (existing.inbounds ?? []) as Record<string, any>[];
      const nonDns = existingInbounds.filter(
        (ib) => !String(ib.tag || '').startsWith('dns-in-')
      );
      return {
        ...existing,
        inbounds: [...nonDns, ...(managed.dnsInbounds ?? [])]
      };
    }
    case '05_routing.json': {
      const rules = ((managed.rules ?? []) as Record<string, any>[]).map((r) => {
        if (r.outboundTag === 'PROXY_TAG') {
          return { ...r, outboundTag: managed.proxyTag };
        }
        return r;
      });
      return {
        ...existing,
        routing: {
          ...(existing.routing as Record<string, any>),
          rules
        }
      };
    }
    case '06_policy.json':
      return {
        ...existing,
        policy: {
          ...(existing.policy as Record<string, any>),
          levels: {
            ...((existing.policy as Record<string, any>)?.levels ?? {}),
            '0': managed.level0
          },
          system: {
            ...((existing.policy as Record<string, any>)?.system ?? {}),
            ...managed.system
          }
        }
      };
    default:
      return existing;
  }
}
