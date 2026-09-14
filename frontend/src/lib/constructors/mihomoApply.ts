import { get } from 'svelte/store';
import { apiFetch, apiFetchJSON } from '../api';
import { showToast, showConfirm, fetchCapabilities } from '../../stores';
import { activateRestartGrace } from '../serviceGrace';
import { t, tp } from '../../i18n';
import {
  findPortCollisions,
  parseXrayPorts,
  parseMihomoPorts,
  parseMihomoListenerPorts,
  type PortAllocation
} from '../portChecker';
import { slugifyProviderName } from '../mihomoYaml';
export interface PreflightWarning {
  code?: string;
  message?: string;
  params?: Record<string, string | number>;
}
import type { MihomoContext } from '../../components/mihomo/MihomoContext.svelte';

function tr(key: string, params?: Record<string, string | number>): string {
  return get(t)(key, params);
}

function trp(baseKey: string, n: number, params?: Record<string, string | number>): string {
  return get(tp)(baseKey, n, params);
}

export function collectListenerPortWarnings(yaml: string, extraYaml?: string): PreflightWarning[] {
  const rawTopPorts = parseMihomoPorts(yaml);
  const topKeys = new Set(rawTopPorts.map((p) => `${p.port}:${p.purpose}`));
  const extraTopPorts = extraYaml
    ? parseMihomoPorts(extraYaml).filter((p) => !topKeys.has(`${p.port}:${p.purpose}`))
    : [];
  const topPorts = [...rawTopPorts, ...extraTopPorts];
  const listenerPorts = parseMihomoListenerPorts(yaml);
  const reserved: PortAllocation[] = [
    { port: 5000, engine: 'mihomo', purpose: 'redir-port' },
    { port: 5001, engine: 'mihomo', purpose: 'tproxy-port' },
    { port: 1053, engine: 'mihomo', purpose: 'dns' }
  ];

  const existingKeys = new Set(topPorts.map((p) => `${p.port}:${p.purpose}`));
  const uniqueReserved = reserved.filter((r) => !existingKeys.has(`${r.port}:${r.purpose}`));
  const allAllocations = [...topPorts, ...uniqueReserved, ...listenerPorts];

  const collisions = findPortCollisions(allAllocations);
  const warnings: PreflightWarning[] = [];
  const seenPorts = new Set<number>();

  for (const group of collisions) {
    const hasListener = group.some((p) => p.purpose.startsWith('listener:'));
    if (!hasListener) continue;

    const portNum = group[0].port;
    if (seenPorts.has(portNum)) continue;
    seenPorts.add(portNum);

    const firstListenerIdx = group.findIndex((p) => p.purpose.startsWith('listener:'));
    const other = group.find((p, idx) => idx !== firstListenerIdx) || group[0];
    const conflictName = other.purpose.startsWith('listener:')
      ? other.purpose.slice(9)
      : other.purpose;

    warnings.push({
      code: 'listener_port_collision',
      message: tr('mihomo.listener_port_collision', {
        port: String(portNum),
        conflict: conflictName
      })
    });
  }

  return warnings;
}

export interface ApplyMihomoOptions {
  ctx: MihomoContext;
  selectedFile?: string;
  capabilitiesKernel?: any;
  onSetValidationError: (err: string) => void;
  onSetSaveWarnings: (warnings: PreflightWarning[], title?: string) => void;
}

export async function applyMihomoConfig({
  ctx,
  selectedFile,
  capabilitiesKernel,
  onSetValidationError,
  onSetSaveWarnings
}: ApplyMihomoOptions): Promise<boolean> {
  // Check port collisions
  let xrayPorts: PortAllocation[] = [];
  try {
    const res = await apiFetch(
      '/api/config/read?path=' + encodeURIComponent('/opt/etc/xray/configs/00_main.json')
    );
    if (res.ok) {
      const text = await res.text();
      xrayPorts = parseXrayPorts(text);
    }
  } catch (e: any) {
    if (e?.status === 401) return false;
  }

  let mihomoPorts: PortAllocation[] = [
    { port: ctx.existingTproxyPort ?? 5001, engine: 'mihomo', purpose: 'tproxy-port' },
    { port: ctx.existingRedirPort ?? 5000, engine: 'mihomo', purpose: 'redir-port' },
    { port: 7890, engine: 'mihomo', purpose: 'mixed-port' }
  ];
  try {
    const resM = await apiFetch(
      '/api/config/read?path=' + encodeURIComponent('/opt/etc/mihomo/config.yaml')
    );
    if (resM.ok) {
      const textM = await resM.text();
      const parsedM = parseMihomoPorts(textM);
      if (parsedM.length > 0) {
        mihomoPorts = parsedM;
      }
    }
  } catch (e: any) {
    if (e?.status === 401) return false;
  }

  const allPorts = [...mihomoPorts, ...xrayPorts];
  const collisions = findPortCollisions(allPorts);
  if (collisions.length > 0) {
    const details = collisions
      .map((group) => {
        const portNum = group[0].port;
        const descriptions = group.map((p) => `${p.engine} (${p.purpose})`).join(' vs ');
        return `Port ${portNum}: ${descriptions}`;
      })
      .join('\n');

    if (
      !(await showConfirm({
        title: tr('editor.port_collision_title'),
        message: details,
        consequence: tr('editor.port_collision_warning'),
        variant: 'danger',
        confirmLabel: tr('app.continue')
      }))
    ) {
      return false;
    }
  }

  try {
    const path = selectedFile || '/opt/etc/mihomo/config.yaml';

    // Save previous state to localStorage for Undo
    let currentYAML = '';
    const readRes = await apiFetch(`/api/config/read?path=${encodeURIComponent(path)}`);
    if (readRes.ok) {
      currentYAML = await readRes.text();
      localStorage.setItem('xcp_prev_mihomo_yaml', currentYAML);
    }

    const yamlContent = ctx.generateYaml(capabilitiesKernel);
    onSetValidationError('');
    onSetSaveWarnings([]);
    const listenerWarnings = collectListenerPortWarnings(yamlContent, currentYAML);

    let mergeRes: { content: string; stats?: any; warnings?: PreflightWarning[] };
    try {
      mergeRes = await apiFetchJSON<{
        content: string;
        stats?: any;
        warnings?: PreflightWarning[];
      }>('/api/config/smart-merge', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({
          type: 'mihomo',
          existing_content: currentYAML,
          template_content: yamlContent,
          target_file: path,
          template_owns_nodes: true
        })
      });
    } catch (mergeErr: any) {
      if (mergeErr?.status === 401) return false;
      console.error('Smart merge failed:', mergeErr);
      showToast('error', tr('editor.smart_merge_failed'));
      return false;
    }

    if (!mergeRes || !mergeRes.content) {
      showToast('error', tr('editor.smart_merge_failed'));
      return false;
    }

    const saveRes = await apiFetch(`/api/config/save?path=${encodeURIComponent(path)}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: mergeRes.content
    });

    if (!saveRes.ok) {
      if (saveRes.status === 422) {
        const resData = await saveRes.json();
        onSetValidationError(resData.error || 'Unknown validation error');
        showToast('error', tr('editor.validation_failed'));
        return false;
      }
      const errorText = await saveRes.text();
      throw new Error(errorText || 'Failed to save config');
    }

    const saveJson = await saveRes.json().catch(() => null);
    const saveData = saveJson?.data ?? saveJson;
    const saveResWarnings = Array.isArray(saveData?.warnings) ? saveData.warnings : [];
    const mergeWarnings = Array.isArray((mergeRes as any)?.warnings)
      ? (mergeRes as any).warnings
      : Array.isArray((mergeRes as any)?.data?.warnings)
        ? (mergeRes as any).data.warnings
        : [];
    const backendWarnings = saveResWarnings.length > 0 ? saveResWarnings : mergeWarnings;
    const allWarnings = [...listenerWarnings, ...backendWarnings];
    onSetSaveWarnings(
      allWarnings,
      allWarnings.length > 0 ? tr('editor.save_warnings_title') : undefined
    );

    let restartUrl = '/api/service/control?action=restart';
    const activeKernel = capabilitiesKernel?.active_kernel;
    if (activeKernel && activeKernel !== 'mihomo') {
      if (
        await showConfirm({
          title: tr('editor.switch_kernel_title'),
          consequence: tr('editor.switch_kernel_confirm', { kernel: activeKernel }),
          variant: 'primary',
          confirmLabel: tr('editor.switch')
        })
      ) {
        restartUrl = '/api/service/control?action=switch_kernel&kernel=mihomo';
      }
    }

    activateRestartGrace(6000);
    const restartRes = await apiFetch(restartUrl, {
      method: 'POST'
    });

    if (!restartRes.ok) {
      throw new Error('Failed to restart service');
    }

    await fetchCapabilities();

    ctx.resetDirty();
    const stats = mergeRes.stats || {};
    showToast(
      'success',
      tr('editor.smart_merge_applied', {
        nodes: trp('editor.smart_merge_applied_nodes', stats.proxies ?? 0),
        providers: trp('editor.smart_merge_applied_providers', stats.proxy_providers ?? 0),
        rules: trp('editor.smart_merge_applied_rules', stats.rules ?? 0),
        userRules: trp('editor.smart_merge_applied_user_rules', stats.user_rules ?? 0)
      })
    );
    return true;
  } catch (err: any) {
    if (err?.status === 401) return false;
    console.error(err);
    showToast('error', err.message || tr('mihomo.save_error'));
    return false;
  }
}

export async function undoMihomoConfig(
  selectedFile: string,
  capabilitiesKernel: any,
  onPopulateFromYaml: (text: string) => void
): Promise<boolean> {
  const prevYAML = localStorage.getItem('xcp_prev_mihomo_yaml');
  if (!prevYAML) return false;
  try {
    const path = selectedFile || '/opt/etc/mihomo/config.yaml';

    const saveRes = await apiFetch(`/api/config/save?path=${encodeURIComponent(path)}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: prevYAML
    });

    if (!saveRes.ok) {
      throw new Error('Failed to save rolled back config');
    }

    onPopulateFromYaml(prevYAML);

    let restartUrl = '/api/service/control?action=restart';
    const activeKernel = capabilitiesKernel?.active_kernel;
    if (activeKernel && activeKernel !== 'mihomo') {
      if (
        await showConfirm({
          title: tr('editor.switch_kernel_title'),
          consequence: tr('editor.switch_kernel_confirm', { kernel: activeKernel }),
          variant: 'primary',
          confirmLabel: tr('editor.switch')
        })
      ) {
        restartUrl = '/api/service/control?action=switch_kernel&kernel=mihomo';
      }
    }

    const restartRes = await apiFetch(restartUrl, {
      method: 'POST'
    });
    if (!restartRes.ok) {
      throw new Error('Failed to restart service');
    }

    await fetchCapabilities();
    showToast('success', tr('editor.undo_success'));
    return true;
  } catch (e: any) {
    if (e?.status === 401) return false;
    showToast('error', `Undo failed: ${e.message}`);
    return false;
  }
}

export function mergeMihomoProviders(dbSubs: any[], parsedProviders: any[]) {
  const dbMapByUrl = new Map<string, any>();
  const dbMapByName = new Map<string, any>();

  function cleanUrl(urlStr: string): string {
    if (!urlStr) return '';
    try {
      const match = urlStr.match(/[?&]url=([^&]+)/);
      if (match) {
        return decodeURIComponent(match[1]).trim().toLowerCase();
      }
      return urlStr.trim().toLowerCase();
    } catch {
      return urlStr.trim().toLowerCase();
    }
  }

  for (const sub of dbSubs) {
    const originalUrl = cleanUrl(sub.url || '');
    if (originalUrl) {
      dbMapByUrl.set(originalUrl, sub);
    }
    const providerName = slugifyProviderName(
      sub.profile_title || '',
      sub.name || '',
      sub.url || '',
      sub.id
    );
    dbMapByName.set(providerName, sub);
  }

  const merged = [...dbSubs];

  for (const p of parsedProviders) {
    const originalUrl = cleanUrl(p.url || '');
    const hasMatch = (originalUrl && dbMapByUrl.has(originalUrl)) || dbMapByName.has(p.name);

    if (!hasMatch) {
      const rawUrl = originalUrl || p.url;
      merged.push({
        id: p.id,
        name: p.name,
        url: rawUrl,
        interval: p.interval,
        enabled: true,
        enable_mihomo: true,
        isVirtual: true,
        rawLines: p.rawLines
      });
    }
  }

  return merged;
}

export function applyMihomoPreset(
  ctx: MihomoContext,
  id: string,
  schema: any,
  silent = false
): { lastAppliedPreset: string; presetBaseline: string } {
  ctx.activePreset = id;

  if (schema && schema.mihomo && schema.mihomo.presets) {
    const p = schema.mihomo.presets.find((x: any) => x.id === id);
    if (p) {
      ctx.activeRuleProvider = p.active_rule_provider || 'none';
      ctx.groups = (p.groups || []).map((g: any) => ({
        id: crypto.randomUUID(),
        name: g.name,
        type: g.type || 'select',
        proxies:
          g.name === 'Selective' || g.name === 'Proxy'
            ? ['DIRECT', ...ctx.proxies.map((pr) => pr.name)]
            : [...(g.proxies || [])],
        includeAll: g.include_all ?? false,
        excludeFilter: g.exclude_filter || '',
        url: g.url || 'https://www.gstatic.com/generate_204',
        interval: g.interval || 300,
        icon: g.icon || '',
        enabled: true,
        hidden: g.hidden ?? false,
        tolerance: g.tolerance ?? undefined,
        maxFailedTimes: g.max_failed_times ?? undefined
      }));
      ctx.rules = (p.rules || []).map((r: any) => ({
        id: crypto.randomUUID(),
        type: r.type,
        value: r.value,
        outbound: r.outbound
      }));
      ctx.selectedMetaRuleSets = new Map();
      if (p.selected_meta_rule_sets) {
        for (const [k, v] of Object.entries(p.selected_meta_rule_sets)) {
          ctx.selectedMetaRuleSets.set(k, v as string);
        }
      }
      const baseline = JSON.stringify({
        activeRuleProvider: ctx.activeRuleProvider,
        groups: ctx.groups.map((g) => ({ name: g.name, type: g.type, enabled: g.enabled })),
        rules: ctx.rules.map((r) => ({ type: r.type, value: r.value, outbound: r.outbound }))
      });
      if (!silent) {
        ctx.markDirty();
        showToast('success', tr('editor.preset_applied'));
      }
      return { lastAppliedPreset: id, presetBaseline: baseline };
    }
  }

  if (id === 'rule-based') {
    ctx.groups = [
      {
        id: crypto.randomUUID(),
        name: 'Selective',
        type: 'select',
        proxies: ['DIRECT', ...ctx.proxies.map((p) => p.name)],
        includeAll: true,
        url: 'https://www.gstatic.com/generate_204',
        interval: 300
      }
    ];
    ctx.rules = [];
    ctx.activeRuleProvider = 'metacubex';
    ctx.selectedMetaRuleSets = new Map([
      ['category-ads-all|geosite', 'REJECT'],
      ['telegram|geoip', 'Selective'],
      ['private|geoip', 'DIRECT']
    ]);
  } else if (id === 'global-proxy') {
    ctx.groups = [
      {
        id: crypto.randomUUID(),
        name: 'GLOBAL',
        type: 'select',
        proxies: ctx.proxies.map((p) => p.name),
        includeAll: true,
        excludeFilter: '',
        url: 'https://www.gstatic.com/generate_204',
        interval: 300
      }
    ];
    ctx.rules = [{ id: crypto.randomUUID(), type: 'MATCH', value: '', outbound: 'GLOBAL' }];
    ctx.activeRuleProvider = 'none';
    ctx.selectedMetaRuleSets = new Map();
  } else if (id === 'zkeen-selective') {
    ctx.applyPreset('zkeen');
    ctx.rules = [
      { id: crypto.randomUUID(), type: 'GEOIP', value: 'ru', outbound: 'DIRECT' },
      { id: crypto.randomUUID(), type: 'MATCH', value: '', outbound: 'DIRECT' }
    ];
  } else if (id === 'only-blocked') {
    ctx.groups = [
      {
        id: crypto.randomUUID(),
        name: 'Selective',
        type: 'select',
        proxies: ['DIRECT', ...ctx.proxies.map((p) => p.name)],
        includeAll: true,
        url: 'https://www.gstatic.com/generate_204',
        interval: 300,
        icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Proxy.png'
      }
    ];
    ctx.rules = [
      {
        id: crypto.randomUUID(),
        type: 'RULE-SET',
        value: 'refilter@domain',
        outbound: 'Selective'
      },
      { id: crypto.randomUUID(), type: 'RULE-SET', value: 'private@ip', outbound: 'DIRECT' },
      { id: crypto.randomUUID(), type: 'MATCH', value: '', outbound: 'DIRECT' }
    ];
    ctx.activeRuleProvider = 'zkeen';
    ctx.selectedMetaRuleSets = new Map();
  }

  const baseline = JSON.stringify({
    activeRuleProvider: ctx.activeRuleProvider,
    groups: ctx.groups.map((g) => ({ name: g.name, type: g.type, enabled: g.enabled })),
    rules: ctx.rules.map((r) => ({ type: r.type, value: r.value, outbound: r.outbound }))
  });
  if (!silent) {
    ctx.markDirty();
    showToast('success', tr('editor.preset_applied'));
  }
  return { lastAppliedPreset: id, presetBaseline: baseline };
}
