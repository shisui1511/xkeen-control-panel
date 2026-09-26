import { writable } from 'svelte/store';

// Пресеты и шаблоны Xray пишут geoip:/geosite: так, будто рядом лежат
// стандартные geoip.dat и geosite.dat. XKeen их не ставит: его базы — это
// geoip_v2fly.dat, geosite_zkeen.dat и т. п. в /opt/etc/xray/dat. Правило со
// ссылкой на отсутствующий файл валит Xray целиком, поэтому ссылки пресетов
// переводятся на реально установленные базы.

/** Частные и служебные сети — замена geoip:private, не требует geoip.dat */
export const PRIVATE_CIDRS = [
  '0.0.0.0/8',
  '10.0.0.0/8',
  '100.64.0.0/10',
  '127.0.0.0/8',
  '169.254.0.0/16',
  '172.16.0.0/12',
  '192.168.0.0/16',
  '224.0.0.0/4',
  '255.255.255.255/32',
  '::1/128',
  'fc00::/7',
  'fe80::/10'
];

/** Файл, которым закрываются ссылки geosite:/geoip:; '' — стандартный .dat */
export interface GeoAvailability {
  geosite: string | null;
  geoip: string | null;
}

/** Пока список баз не загружен, ссылки пресетов остаются как есть */
export const GEO_UNKNOWN: GeoAvailability = { geosite: '', geoip: '' };

/** Установленные базы Xray (имена файлов из /api/dat/list) */
export const xrayGeoAvailability = writable<GeoAvailability>(GEO_UNKNOWN);

function pick(files: Set<string>, kind: 'geosite' | 'geoip'): string | null {
  if (files.has(`${kind}.dat`)) return '';
  const v2fly = `${kind}_v2fly.dat`;
  return files.has(v2fly) ? v2fly : null;
}

export function geoAvailability(files: string[]): GeoAvailability {
  const set = new Set(files);
  return { geosite: pick(set, 'geosite'), geoip: pick(set, 'geoip') };
}

/**
 * adaptGeoValues — ссылки geoip:/geosite: под установленные базы.
 * geoip:private → частные сети; при стандартном .dat ссылка не меняется,
 * при базе v2fly — ext:<файл>:<тег>, без баз — отбрасывается. Прочие значения
 * (домены, CIDR, ext:…) не трогаются.
 */
export function adaptGeoValues(values: string[], avail: GeoAvailability): string[] {
  const out: string[] = [];
  for (const v of values) {
    if (v === 'geoip:private') {
      out.push(...PRIVATE_CIDRS);
      continue;
    }
    const m = /^(geosite|geoip):(.+)$/.exec(v);
    if (!m) {
      out.push(v);
      continue;
    }
    const file = avail[m[1] as 'geosite' | 'geoip'];
    if (file === null) continue;
    out.push(file ? `ext:${file}:${m[2]}` : v);
  }
  return [...new Set(out)];
}

type WithGeo = { ip?: string[]; domain?: string[] };

/**
 * adaptPresetRules — правила пресета под установленные базы. Правило, у
 * которого ip или domain опустел, убирается: без условий оно ловило бы весь
 * трафик.
 */
export function adaptPresetRules<T extends WithGeo>(rules: T[], avail: GeoAvailability): T[] {
  const out: T[] = [];
  for (const rule of rules) {
    const next = { ...rule };
    let emptied = false;
    for (const key of ['ip', 'domain'] as const) {
      const vals = rule[key];
      if (!vals || vals.length === 0) continue;
      const adapted = adaptGeoValues(vals, avail);
      if (adapted.length === 0) emptied = true;
      next[key] = adapted;
    }
    if (!emptied) out.push(next);
  }
  return out;
}

/**
 * adaptDnsServers — DNS-серверы пресета: у сервера для доменов (domains)
 * ссылки переводятся так же; если доменов не осталось, сервер убирается —
 * иначе он стал бы общим резолвером.
 */
export function adaptDnsServers<S>(servers: S[], avail: GeoAvailability): S[] {
  const out: S[] = [];
  for (const s of servers) {
    if (typeof s !== 'object' || s === null) {
      out.push(s);
      continue;
    }
    const srv = s as S & { domains?: string[]; expectIPs?: string[] };
    const next = { ...srv };
    if (srv.domains && srv.domains.length > 0) {
      next.domains = adaptGeoValues(srv.domains, avail);
      if (next.domains.length === 0) continue;
    }
    if (srv.expectIPs && srv.expectIPs.length > 0) {
      next.expectIPs = adaptGeoValues(srv.expectIPs, avail);
      if (next.expectIPs.length === 0) delete next.expectIPs;
    }
    out.push(next);
  }
  return out;
}
