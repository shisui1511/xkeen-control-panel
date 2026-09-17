<script lang="ts">
  import { t } from '../../i18n';
  import Modal from '../Modal.svelte';
  import Icon from '../Icon.svelte';
  import XrayOutboundForm from './XrayOutboundForm.svelte';
  import type { OutboundDetail } from './XrayContext.svelte';
  import { apiFetchJSON } from '../../lib/api';
  import { showToast } from '../../stores';
  import { parseImportLink, generateUniqueTag } from '../../lib/constructors/nodeParser';

  let {
    customOutbounds = $bindable([]),
    subscriptionOutbounds = [],
    outboundDetails = [],
    outboundTags = [],
    onReloadTags,
    onchange
  }: {
    customOutbounds: any[];
    subscriptionOutbounds: any[];
    outboundDetails: OutboundDetail[];
    outboundTags: string[];
    onReloadTags?: () => Promise<void>;
    onchange?: () => void;
  } = $props();

  let showOutboundForm = $state(false);
  let editingOutboundIndex = $state<number | null>(null);
  let currentEditingOutbound = $state<any>({});

  // Import modal states
  let showImportModal = $state(false);
  let importLink = $state('');
  let importStep = $state<1 | 2>(1);
  let importLoading = $state(false);
  let importNodes = $state<any[]>([]);
  let importErrorMsg = $state('');
  let importSource = $state<'links' | 'file' | 'clipboard'>('links');
  let isDraggingFile = $state(false);
  let loadedFileName = $state('');

  function getNodeServer(node: any): string {
    if (!node) return '';
    if (node.address) return node.address;
    if (!node.settings) return '';
    if (node.settings.vnext && node.settings.vnext[0]) {
      return node.settings.vnext[0].address || '';
    }
    if (node.settings.servers && node.settings.servers[0]) {
      return node.settings.servers[0].address || '';
    }
    if (node.settings.peers && node.settings.peers[0]) {
      return node.settings.peers[0].endpoint || '';
    }
    return '';
  }

  function getNodePort(node: any): string {
    if (!node) return '';
    if (node.port) return String(node.port);
    if (!node.settings) return '';
    if (node.settings.vnext && node.settings.vnext[0]) {
      return String(node.settings.vnext[0].port || '');
    }
    if (node.settings.servers && node.settings.servers[0]) {
      return String(node.settings.servers[0].port || '');
    }
    return '';
  }

  function openAddOutbound() {
    editingOutboundIndex = null;
    currentEditingOutbound = {
      tag: '',
      protocol: 'vless',
      address: '',
      port: 443,
      uuid: '',
      flow: 'none',
      network: 'tcp',
      security: 'none',
      tlsServerName: '',
      sni: '',
      realityPublicKey: '',
      realityShortId: '',
      realitySpiderX: '',
      wsPath: '',
      wsHost: '',
      grpcServiceName: '',
      password: '',
      method: 'aes-128-gcm',
      wireguardSecretKey: '',
      wireguardAddress: '',
      wireguardMtu: 1420,
      wireguardReserved: '',
      dialerProxy: ''
    };
    showOutboundForm = true;
  }

  function openEditOutbound(idx: number) {
    if (idx < 0 || idx >= customOutbounds.length) return;
    const item = customOutbounds[idx];
    editingOutboundIndex = idx;

    const f: any = {
      tag: item.tag || '',
      protocol: item.protocol || 'vless',
      address: '',
      port: 443,
      uuid: '',
      flow: 'none',
      network: 'tcp',
      security: 'none',
      tlsServerName: '',
      sni: '',
      realityPublicKey: '',
      realityShortId: '',
      realitySpiderX: '',
      wsPath: '',
      wsHost: '',
      grpcServiceName: '',
      password: '',
      method: 'aes-128-gcm',
      wireguardSecretKey: '',
      wireguardAddress: '',
      wireguardMtu: 1420,
      wireguardReserved: '',
      dialerProxy: ''
    };

    if (item.streamSettings?.sockopt?.dialerProxy) {
      f.dialerProxy = item.streamSettings.sockopt.dialerProxy;
    }

    if (item.protocol === 'vless' || item.protocol === 'vmess') {
      const vnext = item.settings?.vnext?.[0];
      if (vnext) {
        f.address = vnext.address || '';
        f.port = vnext.port || 443;
        const user = vnext.users?.[0];
        if (user) {
          f.uuid = user.id || '';
          f.flow = user.flow || 'none';
        }
      }
    } else if (item.protocol === 'trojan') {
      const srv = item.settings?.servers?.[0];
      if (srv) {
        f.address = srv.address || '';
        f.port = srv.port || 443;
        f.password = srv.password || '';
      }
    } else if (item.protocol === 'shadowsocks') {
      const srv = item.settings?.servers?.[0];
      if (srv) {
        f.address = srv.address || '';
        f.port = srv.port || 443;
        f.password = srv.password || '';
        f.method = srv.method || 'aes-128-gcm';
      }
    } else if (item.protocol === 'wireguard') {
      f.wireguardSecretKey = item.settings?.secretKey || '';
      f.wireguardAddress = Array.isArray(item.settings?.address)
        ? item.settings.address.join(', ')
        : item.settings?.address || '';
      f.wireguardMtu = item.settings?.mtu || 1420;
      const peer = item.settings?.peers?.[0];
      if (peer) {
        const parts = (peer.endpoint || '').split(':');
        f.address = parts[0] || '';
        f.port = Number(parts[1]) || 51820;
        f.password = peer.publicKey || '';
      }
      if (item.settings?.reserved) {
        f.wireguardReserved = item.settings.reserved.join(', ');
      }
    }

    if (item.streamSettings) {
      f.network = item.streamSettings.network || 'tcp';
      f.security = item.streamSettings.security || 'none';
      if (item.streamSettings.tlsSettings) {
        f.tlsServerName = item.streamSettings.tlsSettings.serverName || '';
        f.sni = f.tlsServerName;
      }
      if (item.streamSettings.realitySettings) {
        f.tlsServerName = item.streamSettings.realitySettings.serverName || '';
        f.sni = f.tlsServerName;
        f.realityPublicKey = item.streamSettings.realitySettings.publicKey || '';
        f.realityShortId = item.streamSettings.realitySettings.shortId || '';
        f.realitySpiderX = item.streamSettings.realitySettings.spiderX || '';
      }
      if (item.streamSettings.wsSettings) {
        f.wsPath = item.streamSettings.wsSettings.path || '';
        f.wsHost = item.streamSettings.wsSettings.headers?.Host || '';
      }
      if (item.streamSettings.grpcSettings) {
        f.grpcServiceName = item.streamSettings.grpcSettings.serviceName || '';
      }
    }

    currentEditingOutbound = f;
    showOutboundForm = true;
  }

  function handleSaveOutbound() {
    if (!currentEditingOutbound.tag?.trim()) return;

    const form = currentEditingOutbound;
    const outbound: any = {
      tag: form.tag.trim(),
      protocol: form.protocol
    };

    if (form.protocol === 'vless' || form.protocol === 'vmess') {
      outbound.settings = {
        vnext: [
          {
            address: form.address.trim(),
            port: Number(form.port) || 443,
            users: [
              {
                id: (form.uuid || '').trim(),
                flow:
                  form.protocol === 'vless' && form.flow && form.flow !== 'none'
                    ? form.flow
                    : undefined,
                encryption: form.protocol === 'vless' ? 'none' : undefined
              }
            ]
          }
        ]
      };
    } else if (form.protocol === 'trojan') {
      outbound.settings = {
        servers: [
          {
            address: form.address.trim(),
            port: Number(form.port) || 443,
            password: (form.password || '').trim()
          }
        ]
      };
    } else if (form.protocol === 'shadowsocks') {
      outbound.settings = {
        servers: [
          {
            address: form.address.trim(),
            port: Number(form.port) || 443,
            password: (form.password || '').trim(),
            method: form.method
          }
        ]
      };
    } else if (form.protocol === 'wireguard') {
      if (form.wireguardReserved && form.wireguardReserved.trim()) {
        const parts = form.wireguardReserved
          .split(',')
          .map((s: string) => parseInt(s.trim(), 10))
          .filter((n: number) => !isNaN(n) && n >= 0 && n <= 255);
        if (parts.length !== 3) {
          showToast('error', $t('xray.reserved_invalid'));
          return;
        }
      }
      outbound.settings = {
        secretKey: (form.wireguardSecretKey || '').trim(),
        address: (form.wireguardAddress || '')
          .split(',')
          .map((s: string) => s.trim())
          .filter(Boolean),
        mtu: Number(form.wireguardMtu) || 1420,
        peers: [
          {
            publicKey: (form.password || '').trim(),
            endpoint: `${form.address.trim()}:${form.port}`
          }
        ]
      };
      if (form.wireguardReserved && form.wireguardReserved.trim()) {
        outbound.settings.reserved = form.wireguardReserved
          .split(',')
          .map((s: string) => parseInt(s.trim(), 10))
          .filter((n: number) => !isNaN(n) && n >= 0 && n <= 255);
      }
    }

    if (!['wireguard', 'freedom', 'blackhole'].includes(form.protocol)) {
      const stream: any = {
        network: form.network || 'tcp',
        security: form.security || 'none'
      };
      if (form.security === 'tls') {
        const sni = (form.sni || form.tlsServerName || '').trim();
        stream.tlsSettings = {
          serverName: sni || undefined
        };
      } else if (form.security === 'reality') {
        const sni = (form.sni || form.tlsServerName || '').trim();
        stream.realitySettings = {
          serverName: sni || undefined,
          publicKey: (form.realityPublicKey || '').trim() || undefined,
          shortId: (form.realityShortId || '').trim() || undefined,
          spiderX: (form.realitySpiderX || '').trim() || undefined
        };
      }
      if (form.network === 'ws') {
        stream.wsSettings = {
          path: (form.wsPath || '').trim() || undefined,
          headers: form.wsHost?.trim() ? { Host: form.wsHost.trim() } : undefined
        };
      } else if (form.network === 'grpc') {
        stream.grpcSettings = {
          serviceName: (form.grpcServiceName || '').trim() || undefined
        };
      }
      if (form.dialerProxy) {
        stream.sockopt = { dialerProxy: form.dialerProxy };
      }
      outbound.streamSettings = stream;
    }

    if (editingOutboundIndex !== null) {
      customOutbounds[editingOutboundIndex] = outbound;
    } else {
      customOutbounds.push(outbound);
    }

    showOutboundForm = false;
    onchange?.();
  }

  function removeOutbound(idx: number) {
    if (idx >= 0 && idx < customOutbounds.length) {
      customOutbounds.splice(idx, 1);
      onchange?.();
    }
  }

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
      handleParseImportLink();
    };
    reader.readAsText(file);
  }

  async function handleParseImportLink() {
    const trimmed = importLink.trim();
    if (!trimmed) {
      importErrorMsg = $t('subscr.import_error_empty');
      return;
    }

    importErrorMsg = '';
    importLoading = true;

    try {
      const parseApi = async (payload: { links: string[] }) => {
        return apiFetchJSON('/api/outbound/parse', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
      };

      const result = await parseImportLink(trimmed, parseApi, outboundTags);

      if (result.success && result.nodes.length > 0) {
        importNodes = result.nodes;
        importStep = 2;
      } else {
        importErrorMsg = result.errors.join(', ') || $t('subscr.import_error_invalid');
      }
    } catch (e: any) {
      importErrorMsg = e.message || $t('subscr.import_error_invalid');
    } finally {
      importLoading = false;
    }
  }

  async function confirmImportNode() {
    importErrorMsg = '';
    importLoading = true;

    try {
      const items = importNodes.map((item) => ({
        link: item.link,
        tag: item.tag.trim()
      }));

      await apiFetchJSON('/api/outbound/import-bulk', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ items })
      });

      showToast('success', $t('subscr.import_success', { count: importNodes.length }));
      showImportModal = false;
      await onReloadTags?.();
      onchange?.();
    } catch (e: any) {
      if (e?.status === 401) return;
      importErrorMsg = e.message || $t('subscr.import_error');
    } finally {
      importLoading = false;
    }
  }
</script>

<div class="sec-body">
  <div class="section-title" style="margin-bottom: 12px;">
    {$t('editor.xray_section_outbounds')}
  </div>

  <div
    class="constructor-outbounds-header"
    style="display: flex; gap: 8px; margin-bottom: 12px; flex-wrap: wrap;"
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
    <button type="button" class="add-btn btn-secondary" onclick={openAddOutbound}>
      + {$t('xray.add_outbound_manual')}
    </button>
  </div>

  <div class="outbounds-list">
    <!-- Custom Outbounds (Editable) -->
    {#each customOutbounds as item, idx}
      <div
        class="card tag-card"
        style="margin-bottom: 8px; padding: 12px; display: flex; align-items: center; justify-content: space-between;"
      >
        <div>
          <span class="badge badge-tag">{item.tag}</span>
          <span style="font-size: 0.75rem; color: var(--fg-secondary); margin-left: 8px;">
            ({item.protocol} &bull; {getNodeServer(item)}:{getNodePort(item)})
          </span>
        </div>
        <div style="display: flex; gap: 8px;">
          <button
            type="button"
            class="btn-icon"
            onclick={() => openEditOutbound(idx)}
            title={$t('app.edit')}
          >
            <Icon name="edit" />
          </button>
          <button
            type="button"
            class="btn-icon btn-del"
            onclick={() => removeOutbound(idx)}
            title={$t('app.delete')}
          >
            <Icon name="close" size={12} />
          </button>
        </div>
      </div>
    {/each}

    <!-- System / Subscription Outbounds (Read-Only) -->
    {#each outboundDetails.filter((d) => ['direct', 'block', 'dns-out'].includes(d.tag) || subscriptionOutbounds.some((s) => s.tag === d.tag)) as item}
      <div
        class="card tag-card"
        style="margin-bottom: 8px; padding: 12px; display: flex; align-items: center; justify-content: space-between; opacity: 0.75; background: var(--bg-surface-active);"
      >
        <div>
          <span class="badge badge-tag" style="background: var(--bg-surface);">{item.tag}</span>
          {#if item.server}
            <span style="font-size: 0.75rem; color: var(--fg-secondary); margin-left: 8px;">
              ({item.protocol} &bull; {item.server})
            </span>
          {/if}
        </div>
        <span class="tag-desc" style="font-size: 0.8125rem; color: var(--fg-secondary);">
          {item.tag === 'direct' ? $t('xray.direct_freedom') : ''}
          {item.tag === 'block' ? $t('xray.block_blackhole') : ''}
          {item.tag === 'dns-out' ? $t('xray.dns_requests') : ''}
          {!['direct', 'block', 'dns-out'].includes(item.tag) ? $t('xray.subscription') : ''}
        </span>
      </div>
    {/each}
  </div>

  <div style="margin-top: 12px;">
    <button class="add-btn btn-secondary" onclick={openAddOutbound} type="button">
      + {$t('xray.add_outbound_manual')}
    </button>
  </div>

  <!-- Outbound Modal using XrayOutboundForm -->
  <Modal
    isOpen={showOutboundForm}
    title={editingOutboundIndex !== null
      ? $t('xray.edit_outbound_title', { tag: currentEditingOutbound.tag || 'outbound' })
      : $t('xray.add_outbound_title')}
    onclose={() => (showOutboundForm = false)}
  >
    <XrayOutboundForm
      bind:outbound={currentEditingOutbound}
      isEdit={editingOutboundIndex !== null}
      {outboundDetails}
      existingTags={outboundTags}
      onSave={handleSaveOutbound}
      onCancel={() => (showOutboundForm = false)}
    />
  </Modal>

  <!-- Node Import Modal -->
  <Modal
    isOpen={showImportModal}
    title={$t('subscr.import_modal_title')}
    onclose={closeImportModal}
  >
    <div style="display: flex; flex-direction: column; gap: 16px;">
      {#if importErrorMsg}
        <div
          class="error-msg"
          id="xray-import-error"
          role="alert"
          style="color: var(--danger); margin-bottom: 12px; font-size: 13px;"
        >
          {importErrorMsg}
        </div>
      {/if}

      {#if importStep === 1}
        <div
          class="import-source-tabs"
          style="display: flex; gap: 8px; margin-bottom: 12px; border-bottom: 1px solid var(--border-color); padding-bottom: 8px;"
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
            <label for="import-link" class="form-label">{$t('subscr.import_link_label')}</label>
            <textarea
              id="import-link"
              class="form-input textarea-link"
              aria-invalid={!!importErrorMsg}
              aria-describedby={importErrorMsg ? 'xray-import-error' : undefined}
              bind:value={importLink}
              placeholder={$t('subscr.import_link_placeholder')}
              rows="4"
              style="resize: none; font-family: var(--font-family-mono); font-size: 12px; width: 100%; box-sizing: border-box;"
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
                <span style="color: var(--color-primary); font-weight: 600;">{loadedFileName}</span>
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
            style="padding: 24px; text-align: center; background: var(--bg-surface); border: 1px dashed var(--border-color); border-radius: var(--radius);"
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
                  handleParseImportLink();
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
          <h3 class="preview-title" style="margin: 0 0 12px 0; font-size: 14px;">
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
                  style="background: var(--bg-surface); border: 1px solid var(--danger); border-radius: var(--radius-sm); padding: 10px; display: flex; flex-direction: column; gap: 8px; position: relative;"
                >
                  <button
                    type="button"
                    onclick={() => (importNodes = importNodes.filter((_, i) => i !== idx))}
                    style="position: absolute; right: 10px; top: 10px; background: none; border: 0; color: var(--fg-secondary); cursor: pointer; display: flex; align-items: center;"
                    aria-label={$t('app.remove')}><Icon name="close" size={12} /></button
                  >
                  <div style="font-size: 12px; color: var(--danger); padding-right: 20px;">
                    <strong>{$t('app.error')}:</strong>
                    {item.rowError}
                  </div>
                  <div
                    style="font-size: 0.6875rem; color: var(--fg-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; padding-right: 20px;"
                    title={item.link}
                  >
                    {item.link}
                  </div>
                </div>
              {:else}
                <div
                  class="preview-item-card"
                  style="background: var(--bg-surface); border: 1px solid var(--border-color); border-radius: var(--radius-sm); padding: 10px; display: flex; flex-direction: column; gap: 8px; position: relative;"
                >
                  <button
                    type="button"
                    onclick={() => (importNodes = importNodes.filter((_, i) => i !== idx))}
                    style="position: absolute; right: 10px; top: 10px; background: none; border: 0; color: var(--fg-secondary); cursor: pointer; display: flex; align-items: center;"
                    aria-label={$t('app.remove')}><Icon name="close" size={12} /></button
                  >
                  <div
                    style="display: flex; justify-content: space-between; font-size: 12px; color: var(--fg-secondary); padding-right: 20px;"
                  >
                    <span>
                      <strong style="color: var(--fg-primary);">{item.outbound?.protocol}</strong> · {getNodeServer(
                        item.outbound
                      )}:{getNodePort(item.outbound)}
                    </span>
                  </div>
                  <div style="display: flex; align-items: center; gap: 8px;">
                    <label
                      class="form-label"
                      style="margin: 0; font-size: 12px; flex-shrink: 0;"
                      for="import-tag-{idx}"
                    >
                      {$t('subscr.import_tag_custom')}:
                    </label>
                    <input
                      id="import-tag-{idx}"
                      type="text"
                      class="form-input"
                      bind:value={item.tag}
                      style="flex-grow: 1; font-size: 12px; padding: 4px 8px;"
                    />
                  </div>
                </div>
              {/if}
            {/each}
          </div>
        </div>
      {/if}

      <div style="display: flex; justify-content: flex-end; gap: 12px; margin-top: 16px;">
        <button
          type="button"
          class="btn btn-secondary"
          onclick={closeImportModal}
          disabled={importLoading}
        >
          {$t('app.cancel')}
        </button>
        {#if importStep === 1}
          <button
            type="button"
            class="btn btn-primary"
            onclick={handleParseImportLink}
            disabled={!importLink.trim() || importLoading}
          >
            {#if importLoading}
              <span class="spinner-xs" style="margin-right: 6px;"></span>
            {/if}
            {$t('subscr.import_btn_parse')}
          </button>
        {:else}
          <button
            type="button"
            class="btn btn-primary"
            onclick={confirmImportNode}
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
</div>

<style>
  .sec-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .section-title {
    font-size: 0.9375rem;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .outbounds-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .tag-card {
    padding: 12px;
    border-radius: var(--radius);
    transition: background 0.15s ease;
  }

  .badge-tag {
    font-size: 0.75rem;
    padding: 2px 6px;
    border-radius: var(--radius-xs);
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-weight: 500;
    background: var(--color-primary-subtle, rgba(59, 130, 246, 0.15));
    color: var(--color-primary);
  }

  .btn-icon {
    background: none;
    border: 1px solid var(--border-color);
    border-radius: var(--radius-xs);
    padding: 4px 8px;
    font-size: 12px;
    cursor: pointer;
    color: var(--fg-secondary);
    transition: all 0.15s ease;
    display: inline-flex;
    align-items: center;
  }

  .btn-icon:hover {
    color: var(--fg-primary);
    background: var(--hover);
  }

  .btn-del:hover {
    color: var(--danger) !important;
    border-color: var(--danger) !important;
  }

  .form-label {
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--fg-secondary);
  }

  .form-input {
    background: var(--bg-surface);
    border: 1px solid var(--border-color);
    border-radius: var(--radius-sm);
    padding: 6px 10px;
    font-size: 0.8125rem;
    color: var(--fg-primary);
  }

  .form-input:focus {
    outline: none;
    border-color: var(--color-primary);
  }

  .add-btn {
    background: var(--bg-surface);
    border: 1px dashed var(--border-color);
    border-radius: var(--radius);
    padding: 10px 16px;
    color: var(--fg-secondary);
    font-size: 0.8125rem;
    font-weight: 500;
    cursor: pointer;
    text-align: center;
    transition: all 0.15s ease;
  }

  .add-btn:hover {
    border-color: var(--primary);
    color: var(--primary);
    background: var(--hover);
  }

  .btn-action-primary {
    background: var(--color-primary-subtle, rgba(59, 130, 246, 0.1));
    border: 1px solid var(--color-primary);
    color: var(--color-primary);
  }

  .btn-action-primary:hover {
    background: var(--color-primary);
    color: var(--color-on-primary);
  }

  .conf-dropzone {
    border: 2px dashed var(--border-color);
    border-radius: var(--radius);
    padding: 32px 16px;
    text-align: center;
    cursor: pointer;
    position: relative;
    background: var(--bg-surface);
    transition:
      border-color 0.2s,
      background-color 0.2s;
  }

  .conf-dropzone.dragging {
    border-color: var(--color-primary);
    background: var(--color-primary-subtle, rgba(59, 130, 246, 0.05));
  }

  .file-picker-input {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    opacity: 0;
    cursor: pointer;
  }
</style>
