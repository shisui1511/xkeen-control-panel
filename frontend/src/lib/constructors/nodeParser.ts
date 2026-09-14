import type { Proxy } from '../mihomoYaml';

export interface ImportWizardState {
  step: 1 | 2;
  rawText: string;
  parsedNodes: Array<{
    link: string;
    outbound: any | null;
    tag: string;
    rowError: string | null;
  }>;
  errors: string[];
  loading: boolean;
}

export interface ParseImportLinkResult {
  success: boolean;
  nodes: Array<{
    link: string;
    outbound: any | null;
    tag: string;
    rowError: string | null;
  }>;
  errors: string[];
}

export type ParseApiAdapter = (payload: { links: string[] }) => Promise<any>;

/**
 * Генерирует уникальный тег узла, предотвращая коллизии с уже существующими тегами.
 */
export function generateUniqueTag(baseTag: string, existing: string[]): string {
  const tag = baseTag.trim() || 'node';
  if (!existing.includes(tag)) {
    return tag;
  }
  let counter = 1;
  while (existing.includes(`${tag}-${counter}`)) {
    counter++;
  }
  return `${tag}-${counter}`;
}

/**
 * Валидирует и подтверждает импорт узла с обеспечением уникальности тега.
 */
export function confirmImportNode(
  node: any,
  existingTags: string[]
): { valid: boolean; tag: string } {
  if (!node) {
    return { valid: false, tag: '' };
  }
  const rawTag =
    typeof node === 'string'
      ? node
      : node.tag || node.name || node.outbound?.tag || (node.rowError ? '' : 'node');

  const baseTag = (rawTag || '').trim();
  if (!baseTag) {
    return { valid: false, tag: '' };
  }

  const uniqueTag = generateUniqueTag(baseTag, existingTags);
  return { valid: true, tag: uniqueTag };
}

/**
 * Парсит ссылки узлов через переданный API-адаптер бэкенда (/api/outbound/parse).
 */
export async function parseImportLink(
  rawText: string,
  parseApiFn: ParseApiAdapter,
  existingTags: string[] = []
): Promise<ParseImportLinkResult> {
  const trimmed = (rawText || '').trim();
  if (!trimmed) {
    return {
      success: false,
      nodes: [],
      errors: ['Входной текст пуст']
    };
  }

  // T-122-01: Санитизация и разделение на строки с отсечением пустых
  const lines = trimmed
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => l.length > 0);

  if (lines.length === 0) {
    return {
      success: false,
      nodes: [],
      errors: ['Не найдено строк для импорта']
    };
  }

  try {
    const data = await parseApiFn({ links: lines });
    const parsedItems = (Array.isArray(data) ? data : data?.data) || [];

    if (!parsedItems || parsedItems.length === 0) {
      return {
        success: false,
        nodes: [],
        errors: ['Не удалось распознать узлы из переданных ссылок']
      };
    }

    const currentTags = [...existingTags];
    const nodes: Array<{
      link: string;
      outbound: any | null;
      tag: string;
      rowError: string | null;
    }> = [];
    const errors: string[] = [];

    for (let i = 0; i < parsedItems.length; i++) {
      const result = parsedItems[i];
      const link = lines[i] || '';

      if (result && result.outbound) {
        const baseTag = result.outbound.tag || 'node';
        const uniqueTag = generateUniqueTag(baseTag, currentTags);
        currentTags.push(uniqueTag);

        nodes.push({
          link,
          outbound: result.outbound,
          tag: uniqueTag,
          rowError: result.error || null
        });
      } else {
        const err = result?.error || 'Некорректный формат узла';
        nodes.push({
          link,
          outbound: null,
          tag: '',
          rowError: err
        });
        errors.push(`Строка ${i + 1}: ${err}`);
      }
    }

    const hasSuccessNodes = nodes.some((n) => n.outbound !== null);
    return {
      success: hasSuccessNodes,
      nodes,
      errors
    };
  } catch (err: any) {
    const message = err?.message || 'Ошибка вызова сервиса парсинга узлов';
    return {
      success: false,
      nodes: [],
      errors: [message]
    };
  }
}

/**
 * Преобразует спарсенный Xray outbound объект в формат Proxy для конфигурации Mihomo.
 */
export function mapParsedOutboundToMihomoProxy(parsed: any, customTag?: string): Proxy {
  const proto = (parsed?.protocol || 'vless').toLowerCase();
  const tag = customTag || parsed?.tag || 'imported-node';

  const generateId = () => {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
      return crypto.randomUUID();
    }
    return 'p-' + Math.random().toString(36).substring(2, 11);
  };

  const p: Proxy = {
    id: generateId(),
    name: tag,
    type: 'vless',
    server: '',
    port: 443
  };

  if (!parsed) {
    return p;
  }

  if (proto === 'vless' || proto === 'vmess') {
    p.type = proto;
    const vnext = parsed.settings?.vnext?.[0];
    if (vnext) {
      p.server = vnext.address || '';
      p.port = vnext.port || 443;
      p.uuid = vnext.users?.[0]?.id || '';
      p.flow = vnext.users?.[0]?.flow || '';
    }
    const ss = parsed.streamSettings;
    if (ss) {
      p.tls = ss.security === 'tls' || ss.security === 'reality';
      p.network = ss.network || 'tcp';
      if (ss.wsSettings?.path) {
        p.wsPath = ss.wsSettings.path;
      }
      if (ss.security === 'reality') {
        const rOpts = ss.realitySettings;
        if (rOpts) {
          p.publicKey = rOpts.publicKey || '';
          p.shortId = rOpts.shortId || '';
          p.servername = rOpts.serverName || '';
          p.fingerprint = rOpts.fingerprint || 'chrome';
        }
      } else if (ss.security === 'tls') {
        const tOpts = ss.tlsSettings;
        if (tOpts) {
          p.servername = tOpts.serverName || '';
        }
      }
    }
  } else if (proto === 'shadowsocks' || proto === 'ss') {
    p.type = 'ss';
    const server = parsed.settings?.servers?.[0];
    if (server) {
      p.server = server.address || '';
      p.port = server.port || 443;
      p.cipher = server.method || 'aes-256-gcm';
      p.password = server.password || '';
    }
  } else if (proto === 'hysteria2' || proto === 'hy2') {
    p.type = 'hysteria2';
    const server = parsed.settings?.servers?.[0];
    if (server) {
      p.server = server.address || '';
      p.port = server.port || 443;
      p.password = server.password || '';
      p.sni = parsed.streamSettings?.tlsSettings?.serverName || '';
      p.skipCertVerify = parsed.streamSettings?.tlsSettings?.allowInsecure || false;
      const hy2Settings = parsed.settings?.hysteria2Settings;
      if (hy2Settings?.obfs) {
        p.obfsType = hy2Settings.obfs.type === 'simple' ? 'simple' : 'none';
        p.obfsPassword = hy2Settings.obfs.password || '';
      }
    }
  } else if (proto === 'tuic') {
    p.type = 'tuic';
    const server = parsed.settings?.servers?.[0];
    if (server) {
      p.server = server.address || '';
      p.port = server.port || 443;
      p.uuid = server.uuid || '';
      p.password = server.password || '';
      p.congestion = 'bbr';
      p.sni = parsed.streamSettings?.tlsSettings?.serverName || '';
    }
  } else if (proto === 'wireguard') {
    p.type = 'wireguard';
    const peer = parsed.settings?.peers?.[0];
    if (peer) {
      const ep = peer.endpoint || '';
      if (ep.includes(':')) {
        p.server = ep.substring(0, ep.lastIndexOf(':'));
        p.port = parseInt(ep.substring(ep.lastIndexOf(':') + 1), 10) || 51820;
      } else {
        p.server = ep;
        p.port = 51820;
      }
      p.wgPublicKey = peer.publicKey || '';
      p.wgPresharedKey = peer.preSharedKey || '';
    }
    p.wgPrivateKey = parsed.settings?.secretKey || '';
    p.wgIp = Array.isArray(parsed.settings?.address)
      ? parsed.settings.address.join(', ')
      : parsed.settings?.address || '';
    p.wgMtu = parsed.settings?.mtu || 1280;

    const awg =
      parsed.settings?.amneziaWgOption ||
      parsed.amneziaWgOption ||
      parsed.settings?.awg ||
      parsed.awg;
    if (awg) {
      p.awgEnabled = true;
      if (awg.jc !== undefined) p.awgJc = awg.jc;
      if (awg.jmin !== undefined) p.awgJmin = awg.jmin;
      if (awg.jmax !== undefined) p.awgJmax = awg.jmax;
      if (awg.s1 !== undefined) p.awgS1 = awg.s1;
      if (awg.s2 !== undefined) p.awgS2 = awg.s2;
      if (awg.s3 !== undefined) p.awgS3 = awg.s3;
      if (awg.s4 !== undefined) p.awgS4 = awg.s4;
      if (awg.h1 !== undefined) p.awgH1 = awg.h1;
      if (awg.h2 !== undefined) p.awgH2 = awg.h2;
      if (awg.h3 !== undefined) p.awgH3 = awg.h3;
      if (awg.h4 !== undefined) p.awgH4 = awg.h4;
      if (awg.version !== undefined) p.awgVersion = awg.version;
      if (awg['header-protection-key'] !== undefined) {
        p.awgHeaderProtectionKey = awg['header-protection-key'];
      } else if (awg.headerProtectionKey !== undefined) {
        p.awgHeaderProtectionKey = awg.headerProtectionKey;
      }
      if (awg.i1 !== undefined) p.awgI1 = awg.i1;
      if (awg.i2 !== undefined) p.awgI2 = awg.i2;
      if (awg.i3 !== undefined) p.awgI3 = awg.i3;
      if (awg.i4 !== undefined) p.awgI4 = awg.i4;
      if (awg.i5 !== undefined) p.awgI5 = awg.i5;
    }
  }

  return p;
}

export function getNodeServer(node: any): string {
  if (!node || !node.settings) return '';
  if (node.settings.vnext && node.settings.vnext[0]) {
    return node.settings.vnext[0].address || '';
  }
  if (node.settings.servers && node.settings.servers[0]) {
    return node.settings.servers[0].address || '';
  }
  if (node.settings.peers && node.settings.peers[0]?.endpoint) {
    const ep = node.settings.peers[0].endpoint;
    return ep.includes(':') ? ep.substring(0, ep.lastIndexOf(':')) : ep;
  }
  return '';
}

export function getNodePort(node: any): string {
  if (!node || !node.settings) return '';
  if (node.settings.vnext && node.settings.vnext[0]) {
    return String(node.settings.vnext[0].port || '');
  }
  if (node.settings.servers && node.settings.servers[0]) {
    return String(node.settings.servers[0].port || '');
  }
  if (node.settings.peers && node.settings.peers[0]?.endpoint) {
    const ep = node.settings.peers[0].endpoint;
    return ep.includes(':') ? ep.substring(ep.lastIndexOf(':') + 1) : '51820';
  }
  return '';
}
