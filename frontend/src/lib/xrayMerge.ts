/**
 * xrayMerge.ts — Read-Modify-Write merge для файлов конфигурации Xray.
 *
 * Стратегия: обновлять только управляемые ключи в каждом файле,
 * сохраняя все остальные ключи без изменений (D-04).
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
          ...(existing.log ?? {}),
          loglevel: managed.loglevel,
          dnsLog: managed.dnsLog
        }
      };
    case '02_dns.json':
      return {
        ...existing,
        dns: {
          ...(existing.dns ?? {}),
          servers: managed.servers,
          queryStrategy: managed.queryStrategy,
          hosts: managed.hosts
        }
      };
    case '03_inbounds.json': {
      const existingInbounds = (existing.inbounds ?? []) as Record<string, any>[];
      const nonDns = existingInbounds.filter((ib) => !String(ib.tag || '').startsWith('dns-in-'));
      return {
        ...existing,
        inbounds: [...nonDns, ...(managed.dnsInbounds ?? [])]
      };
    }
    case '05_routing.json': {
      const rules = substituteProxyTag(
        (managed.rules ?? []) as Record<string, any>[],
        managed.proxyTag
      );
      return {
        ...existing,
        routing: {
          ...(existing.routing ?? {}),
          domainStrategy: managed.domainStrategy ?? existing.routing?.domainStrategy,
          rules
        }
      };
    }
    case '06_policy.json':
      return {
        ...existing,
        policy: {
          ...(existing.policy ?? {}),
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

/**
 * syncDnsPipeline — генерирует inbounds (dokodemo-door) и routing rules для тегированных DNS серверов.
 *
 * @param dnsServers - список DNS-серверов
 * @param proxyTag   - основной прокси-выход
 */
export function syncDnsPipeline(
  dnsServers: any[],
  proxyTag: string
): { dnsInbounds: any[]; routingRules: any[] } {
  const dnsInbounds: any[] = [];
  const routingRules: any[] = [];
  let portCounter = 1082;

  for (const srv of dnsServers) {
    if (srv && typeof srv === 'object' && srv.tag) {
      dnsInbounds.push({
        port: portCounter++,
        protocol: 'dokodemo-door',
        settings: { network: 'tcp,udp', followRedirect: true },
        sniffing: { enabled: true, routeOnly: true, destOverride: ['http', 'tls', 'quic'] },
        streamSettings: { sockopt: { tproxy: 'tproxy' } },
        tag: srv.tag
      });

      routingRules.push({
        type: 'field',
        inboundTag: [srv.tag],
        outboundTag: srv.tag === 'dns-in-direct' ? 'direct' : 'PROXY_TAG'
      });
    }
  }

  return { dnsInbounds, routingRules };
}

/**
 * substituteProxyTag — заменяет PROXY_TAG на выбранный outbound тег во всех правилах.
 *
 * @param rules - массив правил маршрутизации
 * @param tag   - реальный тег исходящего соединения
 */
export function substituteProxyTag(rules: any[], tag: string): any[] {
  const realTag = tag || 'direct';
  return rules.map((r) => {
    if (r.outboundTag === 'PROXY_TAG') {
      return { ...r, outboundTag: realTag };
    }
    return r;
  });
}
