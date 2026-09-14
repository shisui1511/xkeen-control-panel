<script lang="ts">
  import Modal from '../Modal.svelte';
  import ProxyForm from './ProxyForm.svelte';
  import AwgDiffCard from '../awg/AwgDiffCard.svelte';
  import { t } from '../../i18n';
  import { showToast, capabilities } from '../../stores';
  import { apiFetch, apiFetchJSON } from '../../lib/api';
  import { getMihomoContext } from './MihomoContext.svelte';
  import { analyzeAwgDiff } from '../../lib/awgPresets';
  import {
    parseImportLink as parseNodesWithApi,
    mapParsedOutboundToMihomoProxy,
    generateUniqueTag,
    getNodeServer,
    getNodePort
  } from '../../lib/constructors/nodeParser';
  import type { Proxy } from '../../lib/mihomoYaml';

  let ctx = getMihomoContext();

  const mihomoVersion = $derived($capabilities?.kernels?.mihomo?.version || '');
  const hasXraySubscriptions = $derived(ctx.subscriptions.some((s) => s.enable_xray));

  // Form state
  let showProxyForm = $state(false);
  let editingProxyId = $state<string | null>(null);
  let isEditingImported = $state(false);

  function newProxyDefaults(type: string): any {
    return {
      name: '',
      type,
      server: '',
      port: type === 'wireguard' ? 51820 : 443,
      uuid: crypto.randomUUID(),
      flow: 'xtls-rprx-vision',
      publicKey: '',
      shortId: '',
      servername: 'www.apple.com',
      password: '',
      sni: '',
      skipCertVerify: false,
      obfsType: 'none',
      obfsPassword: '',
      congestion: 'bbr',
      cipher: 'aes-256-gcm',
      network: 'ws',
      wsPath: '/',
      tls: true,
      fingerprint: 'chrome',
      wgPrivateKey: '',
      wgPublicKey: '',
      wgIp: '',
      wgPresharedKey: '',
      wgMtu: 1420,
      awgEnabled: false,
      awgJc: 4,
      awgJmin: 40,
      awgJmax: 70,
      awgS1: 15,
      awgS2: 40,
      awgH1: 1000000001,
      awgH2: 1000000002,
      awgH3: 1000000003,
      awgH4: 1000000004,
      awgS3: undefined,
      awgS4: undefined,
      awgVersion: undefined,
      awgHeaderProtectionKey: '',
      awgI1: '',
      awgI2: '',
      awgI3: '',
      awgI4: '',
      awgI5: '',
      awgContentPaddingAddition: undefined,
      awgRandomTrailers: false,
      awgDisableCookies: false,
      awgRekeyAfterTime: undefined
    };
  }

  let np = $state<any>(newProxyDefaults('vless'));

  function openAddProxy() {
    editingProxyId = null;
    isEditingImported = false;
    np = newProxyDefaults('vless');
    showProxyForm = true;
  }

  function editProxy(p: Proxy) {
    editingProxyId = p.id;
    isEditingImported = !!p.isImported;
    np = { ...p };
    showProxyForm = true;
  }

  function saveProxy() {
    if (!np.name.trim()) return;
    if (editingProxyId) {
      ctx.updateProxy(editingProxyId, { ...np, name: np.name.trim() });
    } else {
      ctx.addProxy({
        ...np,
        id: crypto.randomUUID(),
        name: np.name.trim()
      });
    }
    showProxyForm = false;
    editingProxyId = null;
  }

  // Import node states
  let showImportModal = $state(false);
  let importLink = $state('');
  let importStep = $state(1);
  let importLoading = $state(false);
  let importNodes: { link: string; outbound: any; tag: string; rowError?: string | null }[] =
    $state([]);
  let importErrorMsg = $state('');
  let importSource = $state<'links' | 'file' | 'clipboard'>('links');
  let isDraggingFile = $state(false);
  let loadedFileName = $state('');

  function openImportModal() {
    showImportModal = true;
    importLink = '';
    importStep = 1;
    importLoading = false;
    importNodes = [];
    importErrorMsg = '';
    importSource = 'links';
    isDraggingFile = false;
    loadedFileName = '';
  }

  function closeImportModal() {
    showImportModal = false;
  }

  function handleConfigFile(file: File) {
    loadedFileName = file.name;
    const reader = new FileReader();
    reader.onload = (e) => {
      const text = (e.target?.result as string) || '';
      importLink = text;
      runParseImportLink();
    };
    reader.readAsText(file);
  }

  async function runParseImportLink() {
    const trimmed = importLink.trim();
    if (!trimmed) {
      importErrorMsg = $t('subscr.import_error_empty');
      return;
    }

    importErrorMsg = '';
    importLoading = true;

    try {
      const res = await parseNodesWithApi(
        trimmed,
        async (payload) =>
          apiFetchJSON('/api/outbound/parse', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
          }),
        ctx.proxies.map((p) => p.name)
      );

      if (res.nodes.length > 0) {
        importNodes = res.nodes;
        importStep = 2;
      } else {
        importErrorMsg = res.errors.join(', ') || $t('subscr.import_error_invalid');
      }
    } catch (e: any) {
      importErrorMsg = e.message || $t('subscr.import_error_invalid');
    } finally {
      importLoading = false;
    }
  }

  function confirmImport() {
    if (importNodes.length === 0 || importNodes.some((n) => n.rowError)) return;

    for (const item of importNodes) {
      const p = mapParsedOutboundToMihomoProxy(item.outbound, item.tag);
      ctx.addProxy(p as Proxy);
    }
    showToast('success', $t('subscr.import_success', { count: importNodes.length }));
    showImportModal = false;
  }

  async function loadSubscriptionProxies() {
    try {
      const res = await apiFetch('/api/subscriptions');
      if (!res.ok) return;
      const subs = await res.json();
      if (!Array.isArray(subs) || subs.length === 0) {
        showToast('info', $t('editor.import_proxies_empty'));
        return;
      }
      let imported = 0;
      for (const sub of subs) {
        if (!sub.enabled) continue;
        const nr = await apiFetch(`/api/subscriptions/nodes?id=${sub.id}`);
        if (!nr.ok) continue;
        const nodes: any[] = await nr.json();
        if (!nodes || nodes.length === 0) continue;

        const mapped = nodes.map((n: any) => {
          const serverRaw: string = n.server || '';
          const lastColon = serverRaw.lastIndexOf(':');
          const server = lastColon > 0 ? serverRaw.substring(0, lastColon) : serverRaw;
          const portStr = lastColon > 0 ? serverRaw.substring(lastColon + 1) : '443';
          const port = parseInt(portStr) || 443;
          return {
            id: crypto.randomUUID(),
            name: n.name || n.tag || `proxy-${imported}`,
            type: (n.protocol || 'vless') as any,
            server,
            port,
            isImported: true,
            uuid: n.uuid || '',
            password: n.password || '',
            flow: n.flow || '',
            publicKey: n.public_key || '',
            shortId: n.short_id || '',
            servername: n.servername || '',
            fingerprint: n.fingerprint || '',
            wsPath: n.ws_path || '',
            cipher: n.cipher || '',
            sni: n.sni || '',
            congestion: n.congestion || '',
            alterID: n.alter_id || 0,
            tls: n.security === 'tls' || n.security === 'reality',
            skipCertVerify: n.insecure || false,
            obfsType: (n.obfs_type || 'none') as any,
            obfsPassword: n.obfs_password || ''
          };
        });
        const existingNames = new Set(ctx.proxies.map((p) => p.name));
        const uniqueMapped = mapped.filter((n) => !existingNames.has(n.name));
        for (const item of uniqueMapped) {
          ctx.addProxy(item as Proxy);
        }
        imported += uniqueMapped.length;
      }
      if (imported > 0) {
        showToast('success', $t('editor.import_proxies_done'));
      } else {
        showToast('info', $t('editor.import_proxies_empty'));
      }
    } catch {
      showToast('error', $t('editor.import_proxies_error'));
    }
  }

  async function loadSubscriptions() {
    try {
      const res = await apiFetch('/api/subscriptions');
      if (!res.ok) return;
      const subs = await res.json();
      if (Array.isArray(subs)) {
        ctx.subscriptions = subs.filter((s) => s.enabled);
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      console.error(e);
    }
  }
</script>

<div class="sec-body" data-testid="mihomo-section-proxies">
  <div
    class="constructor-proxy-list"
    style="display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 12px;"
  >
    <button type="button" class="add-btn btn-action-primary" onclick={openImportModal}>
      <svg
        width="13"
        height="13"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        style="margin-right: 4px; display: inline-block; vertical-align: middle;"
      >
        <path
          d="M4 14.899A7 7 0 1 1 15.71 8h1.79a4.5 4.5 0 0 1 2.5 8.242M12 12V22M12 12L15 15M12 12L9 15"
        />
      </svg>
      {$t('subscr.import_node')}
    </button>
    <button type="button" class="add-btn btn-secondary" onclick={openAddProxy}>
      + {$t('mihomo.add_proxy_manual')}
    </button>
    <button
      type="button"
      class="add-btn import-btn"
      onclick={loadSubscriptionProxies}
      disabled={!hasXraySubscriptions}
      title={hasXraySubscriptions ? $t('mihomo.import_xray_desc') : $t('mihomo.no_xray_subs')}
    >
      ↓ {$t('editor.constructor_import_proxies')}
    </button>
  </div>

  {#each ctx.proxies as p (p.id)}
    <div class="item-row" class:item-disabled={p.enabled === false}>
      <span class="item-badge type-{p.type}">{p.type}</span>
      <span class="item-name">{p.name}</span>
      <span class="item-meta">{p.server}:{p.port}</span>

      <label
        class="switch item-switch"
        title={p.enabled === false ? $t('mihomo.proxy_disabled') : $t('mihomo.proxy_enabled')}
      >
        <input
          type="checkbox"
          checked={p.enabled !== false}
          onchange={() => ctx.toggleProxy(p.id)}
        />
        <span class="slider round"></span>
      </label>

      <div class="item-actions">
        <button type="button" class="item-btn" onclick={() => editProxy(p)} title={$t('app.edit')}>
          ✎
        </button>
        <button
          type="button"
          class="item-btn"
          onclick={() => ctx.duplicateProxy(p.id)}
          title={$t('app.duplicate')}
        >
          ⎘
        </button>
        <button
          type="button"
          class="item-btn item-btn-danger"
          onclick={() => ctx.removeProxy(p.id)}
          title={$t('app.delete')}
        >
          ✕
        </button>
      </div>
    </div>
  {/each}

  {#if ctx.mihomoProviders && ctx.mihomoProviders.length > 0}
    <div
      class="sec-subtitle"
      style="margin-top: 16px; margin-bottom: 8px; border-top: 1px solid var(--border); padding-top: 12px; font-weight: 600; font-size: 13px; color: var(--fg-secondary);"
    >
      {$t('mihomo.proxy_providers_hint')}
    </div>
    {#each ctx.mihomoProviders as sub}
      <div class="item-row" style="border-left: 3px solid var(--success);">
        <span
          class="item-badge type-mihomo"
          style="background: rgba(16, 185, 129, 0.15); color: var(--success); border-color: rgba(16, 185, 129, 0.3);"
          >mihomo</span
        >
        <span class="item-name">{sub.name}</span>
        <span
          class="item-meta"
          title={sub.url}
          style="max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;"
        >
          {sub.url}
        </span>
        <button
          type="button"
          class="item-btn"
          onclick={loadSubscriptions}
          title={$t('mihomo.refresh_provider')}
        >
          ⟳
        </button>
      </div>
    {/each}
  {/if}
</div>

<!-- Modal Proxy Form -->
<Modal
  isOpen={showProxyForm}
  title={editingProxyId
    ? $t('mihomo.edit_proxy_title', { name: np.name || 'proxy' })
    : $t('mihomo.new_proxy_title')}
  onclose={() => {
    showProxyForm = false;
    editingProxyId = null;
    isEditingImported = false;
  }}
>
  <ProxyForm
    bind:np
    isEdit={!!editingProxyId}
    isImported={isEditingImported}
    allProxyNames={ctx.proxies.filter((p) => p.id !== editingProxyId).map((p) => p.name)}
    onSave={saveProxy}
    onCancel={() => {
      showProxyForm = false;
      editingProxyId = null;
      isEditingImported = false;
    }}
  />
</Modal>

<!-- Modal Import Nodes -->
<Modal isOpen={showImportModal} title={$t('subscr.import_modal_title')} onclose={closeImportModal}>
  <div style="display: flex; flex-direction: column; gap: 16px;">
    {#if importErrorMsg}
      <div
        class="error-msg"
        id="mihomo-import-error"
        role="alert"
        style="color: var(--danger); margin-bottom: 12px; font-size: 13px;"
      >
        {importErrorMsg}
      </div>
    {/if}

    {#if importStep === 1}
      <div
        class="import-source-tabs"
        style="display: flex; gap: 8px; margin-bottom: 12px; border-bottom: 1px solid var(--border); padding-bottom: 8px;"
      >
        <button
          type="button"
          class="btn btn-xs"
          class:btn-primary={importSource === 'links'}
          class:btn-secondary={importSource !== 'links'}
          onclick={() => (importSource = 'links')}
        >
          {$t('subscr.import_source_links')}
        </button>
        <button
          type="button"
          class="btn btn-xs"
          class:btn-primary={importSource === 'file'}
          class:btn-secondary={importSource !== 'file'}
          onclick={() => (importSource = 'file')}
        >
          {$t('subscr.import_source_file')}
        </button>
        <button
          type="button"
          class="btn btn-xs"
          class:btn-primary={importSource === 'clipboard'}
          class:btn-secondary={importSource !== 'clipboard'}
          onclick={() => (importSource = 'clipboard')}
        >
          {$t('subscr.import_source_clipboard')}
        </button>
      </div>

      {#if importSource === 'links'}
        <div class="form-group">
          <label for="mihomo-import-link" class="form-label">{$t('subscr.import_link_label')}</label
          >
          <textarea
            id="mihomo-import-link"
            class="input textarea-link"
            aria-invalid={!!importErrorMsg}
            aria-describedby={importErrorMsg ? 'mihomo-import-error' : undefined}
            bind:value={importLink}
            placeholder={$t('subscr.import_link_placeholder')}
            rows="4"
            style="resize: none; font-family: var(--font-mono); font-size: 12px; width: 100%; box-sizing: border-box; background: var(--bg-elevated); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 8px; color: var(--fg-primary);"
          ></textarea>
        </div>
      {:else if importSource === 'file'}
        <div
          class="conf-dropzone"
          class:dragging={isDraggingFile}
          role="region"
          aria-label={$t('app.dropzone')}
          ondragover={(e) => {
            e.preventDefault();
            isDraggingFile = true;
          }}
          ondragleave={() => {
            isDraggingFile = false;
          }}
          ondrop={(e) => {
            e.preventDefault();
            isDraggingFile = false;
            const f = e.dataTransfer?.files?.[0];
            if (f) handleConfigFile(f);
          }}
        >
          <div style="font-size: 24px;">📄</div>
          <div style="font-size: 13px; color: var(--fg-secondary);">
            {#if loadedFileName}
              <span style="color: var(--primary); font-weight: 600;">{loadedFileName}</span>
              <span> ({$t('subscr.import_file_loaded')})</span>
            {:else}
              {$t('subscr.import_drop_or_select')}
            {/if}
          </div>
          <input
            type="file"
            accept=".conf,.txt"
            class="file-picker-input"
            onchange={(e) => {
              const f = e.currentTarget.files?.[0];
              if (f) handleConfigFile(f);
            }}
          />
        </div>
      {:else if importSource === 'clipboard'}
        <div
          style="padding: 24px; text-align: center; background: var(--bg-elevated); border: 1px dashed var(--border); border-radius: var(--radius);"
        >
          <p style="font-size: 13px; color: var(--fg-secondary); margin-bottom: 12px;">
            {$t('subscr.import_clipboard_desc')}
          </p>
          <button
            type="button"
            class="btn btn-primary"
            onclick={async () => {
              try {
                const text = await navigator.clipboard.readText();
                if (!text.trim()) {
                  importErrorMsg = $t('subscr.import_clipboard_empty');
                  return;
                }
                importLink = text;
                runParseImportLink();
              } catch (err: any) {
                importErrorMsg = err.message || 'Clipboard access denied';
              }
            }}
          >
            📋 {$t('subscr.import_source_clipboard')}
          </button>
        </div>
      {/if}
    {:else if importStep === 2 && importNodes.length > 0}
      <div class="preview-section">
        <h3 class="preview-title" style="margin: 0 0 12px 0;">
          {$t('subscr.import_preview_title')}
        </h3>
        <div
          class="preview-list"
          style="max-height: 260px; overflow-y: auto; display: flex; flex-direction: column; gap: 10px; padding-right: 4px; scrollbar-width: thin;"
        >
          {#each importNodes as item, idx}
            {#if item.rowError}
              <div
                class="preview-item-card"
                style="background: var(--bg-card); border: 1px solid var(--danger); border-radius: var(--radius-sm); padding: 10px; display: flex; flex-direction: column; gap: 8px; position: relative;"
              >
                <button
                  type="button"
                  onclick={() => (importNodes = importNodes.filter((_, i) => i !== idx))}
                  style="position: absolute; right: 10px; top: 10px; background: none; border: 0; color: var(--fg-secondary); cursor: pointer; font-size: 12px;"
                  aria-label={$t('app.remove')}>✕</button
                >
                <div style="font-size: 12px; color: var(--danger); padding-right: 20px;">
                  <strong>{$t('app.error')}:</strong>
                  {item.rowError}
                </div>
                <div
                  style="font-size: var(--font-size-xs); color: var(--fg-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; padding-right: 20px;"
                  title={item.link}
                >
                  {item.link}
                </div>
              </div>
            {:else}
              <div
                class="preview-item-card"
                style="background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 10px; display: flex; flex-direction: column; gap: 8px; position: relative;"
              >
                <button
                  type="button"
                  onclick={() => (importNodes = importNodes.filter((_, i) => i !== idx))}
                  style="position: absolute; right: 10px; top: 10px; background: none; border: 0; color: var(--fg-secondary); cursor: pointer; font-size: 12px;"
                  aria-label={$t('app.remove')}>✕</button
                >
                <div
                  style="display: flex; justify-content: space-between; font-size: 12px; color: var(--fg-secondary); padding-right: 20px;"
                >
                  <span
                    ><strong style="color: var(--fg-primary);">{item.outbound?.protocol}</strong> · {getNodeServer(
                      item.outbound
                    )}:{getNodePort(item.outbound)}</span
                  >
                </div>
                <div style="display: flex; align-items: center; gap: 8px;">
                  <label
                    class="form-label"
                    style="margin: 0; font-size: 12px; flex-shrink: 0;"
                    for="import-tag-{idx}">{$t('subscr.import_tag_custom')}:</label
                  >
                  <input
                    id="import-tag-{idx}"
                    type="text"
                    class="input"
                    bind:value={item.tag}
                    style="flex-grow: 1; font-size: 12px; box-sizing: border-box; background: var(--bg-elevated); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 4px 8px; color: var(--fg-primary); width: auto;"
                  />
                </div>
                {#if item.outbound?.protocol === 'wireguard' || item.outbound?.settings?.amneziaWgOption || item.outbound?.amneziaWgOption}
                  {@const awgOpts =
                    item.outbound?.settings?.amneziaWgOption ||
                    item.outbound?.amneziaWgOption ||
                    {}}
                  {@const diff = analyzeAwgDiff(awgOpts, 'mihomo', mihomoVersion)}
                  <AwgDiffCard {diff} targetKernel="mihomo" compact />
                {/if}
              </div>
            {/if}
          {/each}
        </div>
      </div>
    {/if}

    <div style="display: flex; justify-content: flex-end; gap: 12px; margin-top: 16px;">
      <button class="btn btn-secondary" onclick={closeImportModal} disabled={importLoading}>
        {$t('app.cancel')}
      </button>
      {#if importStep === 1}
        <button
          class="btn btn-primary"
          onclick={runParseImportLink}
          disabled={!importLink.trim() || importLoading}
        >
          {#if importLoading}
            <span class="spinner-xs" style="margin-right: 6px;"></span>
          {/if}
          {$t('subscr.import_btn_parse')}
        </button>
      {:else}
        <button
          class="btn btn-primary"
          onclick={confirmImport}
          disabled={importLoading ||
            importNodes.length === 0 ||
            importNodes.some((n) => n.rowError)}
        >
          {#if importLoading}
            <span class="spinner-xs" style="margin-right: 6px;"></span>
          {/if}
          {$t('mihomo.import_count', { count: importNodes.length })}
        </button>
      {/if}
    </div>
  </div>
</Modal>

<style>
  .sec-body {
    padding: 16px;
  }

  .add-btn {
    font-size: 12px;
    padding: 6px 12px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: all var(--transition-fast);
    display: inline-flex;
    align-items: center;
    border: 1px solid transparent;
  }

  .btn-action-primary {
    background: var(--primary);
    color: var(--primary-fg);
  }

  .btn-action-primary:hover {
    filter: brightness(1.1);
  }

  .btn-secondary {
    background: var(--bg-card);
    border: 1px solid var(--border);
    color: var(--fg-primary);
  }

  .btn-secondary:hover {
    background: var(--bg-hover);
  }

  .import-btn {
    background: rgba(16, 185, 129, 0.1);
    color: var(--success);
    border: 1px solid rgba(16, 185, 129, 0.2);
  }

  .import-btn:hover:not(:disabled) {
    background: rgba(16, 185, 129, 0.2);
  }

  .item-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 12px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    margin-bottom: 6px;
    transition: all var(--transition-fast);
  }

  .item-row:hover {
    border-color: var(--border-hover);
  }

  .item-row.item-disabled {
    opacity: 0.5;
  }

  .item-badge {
    font-size: var(--font-size-xs);
    padding: 2px 6px;
    border-radius: 4px;
    font-weight: 500;
    text-transform: uppercase;
    flex-shrink: 0;
  }

  .type-vless {
    background: rgba(41, 194, 240, 0.15);
    color: var(--primary);
  }
  .type-hysteria2 {
    background: rgba(70, 209, 138, 0.15);
    color: var(--success);
  }
  .type-tuic {
    background: rgba(240, 180, 80, 0.15);
    color: var(--warning);
  }
  .type-ss {
    background: rgba(239, 91, 107, 0.15);
    color: var(--danger);
  }
  .type-vmess {
    background: rgba(255, 255, 255, 0.08);
    color: var(--fg-secondary);
  }
  .type-trojan {
    background: rgba(236, 72, 153, 0.15);
    color: var(--seq-4);
  }
  .type-wireguard {
    background: rgba(168, 85, 247, 0.15);
    color: var(--seq-3);
  }

  .item-name {
    flex: 1;
    min-width: 50px;
    font-size: 13px;
    font-weight: 500;
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .item-meta {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    flex-shrink: 0;
  }

  .item-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-left: auto;
  }

  .item-btn {
    background: none;
    border: none;
    color: var(--fg-faint);
    cursor: pointer;
    font-size: 12px;
    padding: 4px 6px;
    border-radius: var(--radius-sm);
    transition: all var(--transition-fast);
  }

  .item-btn:hover {
    color: var(--fg-primary);
    background: rgba(255, 255, 255, 0.05);
  }

  .item-btn-danger:hover {
    color: var(--danger);
    background: rgba(239, 91, 107, 0.1);
  }

  .item-switch {
    margin-left: 8px;
  }

  .switch {
    position: relative;
    display: inline-block;
    width: 32px;
    height: 18px;
    flex-shrink: 0;
  }

  .switch input {
    opacity: 0;
    width: 0;
    height: 0;
  }

  .slider {
    position: absolute;
    cursor: pointer;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background-color: rgba(255, 255, 255, 0.1);
    transition: 0.4s;
    border: 1px solid var(--border);
  }

  .slider:before {
    position: absolute;
    content: '';
    height: 12px;
    width: 12px;
    left: 2px;
    bottom: 2px;
    background-color: var(--fg-secondary);
    transition: 0.4s;
  }

  input:checked + .slider {
    background-color: var(--success);
    border-color: var(--success);
  }

  input:checked + .slider:before {
    transform: translateX(14px);
    background-color: var(--bg-page);
  }

  .slider.round {
    border-radius: 18px;
  }

  .slider.round:before {
    border-radius: 50%;
  }

  .conf-dropzone {
    border: 2px dashed var(--border);
    border-radius: var(--radius);
    padding: 24px 16px;
    text-align: center;
    background: var(--bg-elevated);
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    cursor: pointer;
  }

  .conf-dropzone.dragging {
    border-color: var(--primary);
    background: rgba(41, 194, 240, 0.08);
  }

  .file-picker-input::file-selector-button {
    background: var(--bg-elevated);
    color: var(--fg-primary);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 6px 12px;
    font-size: 12px;
    cursor: pointer;
    margin-right: 8px;
  }
</style>
