<script lang="ts">
  import { t } from '../../i18n';
  import { showToast } from '../../stores';
  import { apiFetch } from '../../lib/api';
  import { shadowsocksCiphers } from '../../schemas/xray';
  import Select from '../Select.svelte';
  import AwgDiffCard from '../awg/AwgDiffCard.svelte';
  import { analyzeAwgDiff } from '../../lib/awgPresets';
  import type { OutboundDetail } from './XrayContext.svelte';

  interface Props {
    outbound: any;
    existingTags?: string[];
    outboundDetails?: OutboundDetail[];
    onSave: () => void;
    onCancel: () => void;
    isEdit?: boolean;
  }

  let {
    outbound = $bindable(),
    existingTags = [],
    outboundDetails = [],
    onSave,
    onCancel,
    isEdit = false
  }: Props = $props();

  let generatingUUID = $state(false);
  let generatingRealityKeys = $state(false);

  let tlsPingRunning = $state(false);
  let tlsPingResult = $state<{
    ok: boolean;
    tls_version?: string;
    cipher_suite?: string;
    alpn?: string;
    peer_cn?: string;
    dns_names?: string[];
    not_before?: string;
    not_after?: string;
    days_until_expiry?: number;
    chain_length?: number;
    handshake_ms?: number;
    error?: string;
  } | null>(null);
  let tlsPingError = $state<string | null>(null);

  function generateShadowsocksKey(method: string): string {
    const is16 = method.includes('128');
    const byteLen = is16 ? 16 : 32;
    const array = new Uint8Array(byteLen);
    crypto.getRandomValues(array);
    let binary = '';
    for (let i = 0; i < array.length; i++) {
      binary += String.fromCharCode(array[i]);
    }
    return btoa(binary);
  }

  async function generateUUID() {
    generatingUUID = true;
    try {
      const res = await apiFetch('/api/xray/uuid');
      const data = await res.json();
      if (res.ok && data?.data?.uuid) {
        outbound.uuid = data.data.uuid;
        showToast('success', $t('xray.uuid_generated'));
      } else {
        outbound.uuid = crypto.randomUUID();
      }
    } catch {
      outbound.uuid = crypto.randomUUID();
    } finally {
      generatingUUID = false;
    }
  }

  async function generateRealityKeys() {
    generatingRealityKeys = true;
    try {
      const res = await apiFetch('/api/xray/reality/keygen');
      const data = await res.json();
      if (res.ok && data?.data) {
        outbound.publicKey = data.data.public_key;
        outbound.shortId = data.data.short_id;
        showToast('success', $t('xray.reality_keys_generated'));
      } else {
        showToast('error', data?.error || 'Failed to generate Reality keys');
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', e.message);
    } finally {
      generatingRealityKeys = false;
    }
  }

  async function runTLSPing() {
    if (!outbound.address.trim()) return;
    tlsPingRunning = true;
    tlsPingResult = null;
    tlsPingError = null;
    try {
      let dest = outbound.address.trim();
      if (!dest.includes(':')) {
        dest = `${dest}:${outbound.port || 443}`;
      }
      const serverName = (outbound.sni || outbound.address).trim();
      const alpnList = (outbound as any).alpn ? [(outbound as any).alpn] : [];

      const res = await apiFetch('/api/xray/tls-ping', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          dest,
          server_name: serverName,
          alpn: alpnList,
          insecure: (outbound as any).insecure || false
        })
      });
      const data = await res.json();
      if (res.status === 401) return;
      if (!res.ok) {
        tlsPingError = data?.error || $t('xray.tls_ping.error');
        return;
      }
      tlsPingResult = data?.data || data;
    } catch (e: any) {
      if (e?.status === 401) return;
      tlsPingError = e.message || $t('xray.tls_ping.error');
    } finally {
      tlsPingRunning = false;
    }
  }

  function computeDialerChain(
    currentTag: string,
    targetProxy: string
  ): { chain: string[]; hasCycle: boolean } {
    if (!targetProxy) {
      return { chain: [currentTag, 'DIRECT'], hasCycle: false };
    }
    const chain: string[] = [currentTag];
    const visited = new Set<string>([currentTag]);
    let curr = targetProxy;

    while (curr) {
      chain.push(curr);
      if (visited.has(curr)) {
        return { chain, hasCycle: true };
      }
      visited.add(curr);
      // find next proxy in details
      const found = outboundDetails.find((d) => d.tag === curr);
      curr = (found as any)?.dialerProxy || '';
      if (chain.length > 20) {
        return { chain, hasCycle: true };
      }
    }
    chain.push('DIRECT');
    return { chain, hasCycle: false };
  }

  const dialerChainPreview = $derived.by(() => {
    const tag = outbound.tag?.trim() || 'CURRENT';
    return computeDialerChain(tag, outbound.dialerProxy?.trim() || '');
  });
</script>

<div class="xray-outbound-form">
  <div class="form-row">
    <label class="form-label" for="outbound-tag">{$t('xray.tag_name')} *</label>
    <input id="outbound-tag" class="form-input" bind:value={outbound.tag} placeholder="PROXY" />
  </div>

  <div class="form-row">
    <label class="form-label" for="outbound-protocol">{$t('xray.protocol')}</label>
    <Select id="outbound-protocol" class="form-select" bind:value={outbound.protocol}>
      <option value="vless">VLESS</option>
      <option value="vmess">VMess</option>
      <option value="shadowsocks">Shadowsocks</option>
      <option value="wireguard">WireGuard</option>
    </Select>
  </div>

  <!-- Protocol specific fields -->
  {#if outbound.protocol === 'vless' || outbound.protocol === 'vmess'}
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="outbound-address">
          {$t('xray.server_address')} *
        </label>
        <input
          id="outbound-address"
          class="form-input"
          bind:value={outbound.address}
          placeholder="server.com"
        />
      </div>
      <div class="form-col">
        <label class="form-label" for="outbound-port">{$t('xray.port')} *</label>
        <input
          id="outbound-port"
          class="form-input"
          type="number"
          bind:value={outbound.port}
          min="1"
          max="65535"
        />
      </div>
    </div>

    <div class="form-row">
      <label class="form-label" for="outbound-uuid">{$t('xray.uuid_label')} *</label>
      <div class="input-with-btn">
        <input
          id="outbound-uuid"
          class="form-input"
          bind:value={outbound.uuid}
          placeholder="uuid"
        />
        <button
          class="btn btn-secondary btn-inset"
          onclick={generateUUID}
          disabled={generatingUUID}
          title={$t('app.generate')}
          aria-label={$t('app.generate')}
          data-testid="outbound-uuid-generate"
          type="button"
        >
          {#if generatingUUID}
            <span
              class="spinner"
              style="--spinner-size: 14px; --spinner-track: currentColor; --spinner-color: transparent;"
            ></span>
          {:else}
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              aria-hidden="true"
            >
              <polyline points="23 4 23 10 17 10" />
              <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10" />
            </svg>
          {/if}
        </button>
      </div>
    </div>

    {#if outbound.protocol === 'vless'}
      <div class="form-row">
        <label class="form-label" for="outbound-flow">{$t('xray.flow')}</label>
        <Select id="outbound-flow" class="form-select" bind:value={outbound.flow}>
          <option value="">{$t('app.none')}</option>
          <option value="xtls-rprx-vision">xtls-rprx-vision</option>
        </Select>
      </div>
    {:else if outbound.protocol === 'vmess'}
      <div class="form-row2">
        <div class="form-col">
          <label class="form-label" for="outbound-cipher">{$t('xray.cipher')}</label>
          <Select id="outbound-cipher" class="form-select" bind:value={outbound.cipher}>
            <option value="auto">auto</option>
            <option value="aes-128-gcm">aes-128-gcm</option>
            <option value="chacha20-poly1305">chacha20-poly1305</option>
            <option value="none">none</option>
          </Select>
        </div>
        <div class="form-col">
          <label class="form-label" for="outbound-alterid">AlterID</label>
          <input
            id="outbound-alterid"
            class="form-input"
            type="number"
            bind:value={outbound.alterId}
            min="0"
          />
        </div>
      </div>
    {/if}

    <!-- Security Settings -->
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="outbound-security">{$t('xray.security')}</label>
        <Select id="outbound-security" class="form-select" bind:value={outbound.security}>
          <option value="none">none</option>
          <option value="tls">TLS</option>
          {#if outbound.protocol === 'vless'}
            <option value="reality">REALITY</option>
          {/if}
        </Select>
      </div>
      <div class="form-col">
        <label class="form-label" for="outbound-sni">SNI (ServerName)</label>
        <input
          id="outbound-sni"
          class="form-input"
          bind:value={outbound.sni}
          placeholder="yahoo.com"
        />
      </div>
    </div>

    {#if outbound.security === 'reality' || outbound.security === 'tls'}
      <div class="security-tools-row">
        {#if outbound.security === 'reality'}
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            onclick={generateRealityKeys}
            disabled={generatingRealityKeys}
          >
            {generatingRealityKeys ? $t('xray.generating') : $t('xray.generate_reality_keys')}
          </button>
        {/if}
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          data-testid="tls-ping-btn"
          onclick={runTLSPing}
          disabled={tlsPingRunning || !outbound.address.trim()}
          title={$t('xray.tls_ping.hint')}
        >
          {tlsPingRunning ? $t('xray.tls_ping.running') : $t('xray.tls_ping.button')}
        </button>
      </div>

      {#if tlsPingError}
        <div class="alert alert-error" data-testid="tls-ping-error" style="margin-bottom: 12px;">
          {tlsPingError}
        </div>
      {/if}

      {#if tlsPingResult}
        <div class="card tls-ping-result" data-testid="tls-ping-result">
          <div class="tls-ping-head">
            <span class="tls-ping-title">{$t('xray.tls_ping.title')}</span>
            {#if tlsPingResult.ok}
              <span class="badge badge-tag badge-direct">
                OK ({tlsPingResult.handshake_ms}ms)
              </span>
            {:else}
              <span class="badge badge-tag badge-block">FAIL</span>
            {/if}
          </div>

          {#if !tlsPingResult.ok && tlsPingResult.error}
            <div class="text-danger tls-ping-failure" data-testid="tls-ping-failure">
              {tlsPingResult.error}
            </div>
          {/if}

          {#if tlsPingResult.ok}
            <div class="tls-grid">
              <div>
                <span class="text-muted">{$t('xray.tls_ping.version')}:</span>
                <span data-testid="tls-ping-version">{tlsPingResult.tls_version || '—'}</span>
              </div>
              <div>
                <span class="text-muted">{$t('xray.tls_ping.alpn')}:</span>
                <span data-testid="tls-ping-alpn">{tlsPingResult.alpn || '—'}</span>
              </div>
              <div>
                <span class="text-muted">{$t('xray.tls_ping.cipher')}:</span>
                <span data-testid="tls-ping-cipher">{tlsPingResult.cipher_suite || '—'}</span>
              </div>
              <div>
                <span class="text-muted">{$t('xray.tls_ping.peer_cn')}:</span>
                <span data-testid="tls-ping-cn">{tlsPingResult.peer_cn || '—'}</span>
              </div>
              <div>
                <span class="text-muted">{$t('xray.tls_ping.expires')}:</span>
                <span data-testid="tls-ping-expires">{tlsPingResult.not_after || '—'}</span>
              </div>
              <div>
                <span class="text-muted">{$t('xray.tls_ping.days_left')}:</span>
                {#if tlsPingResult.days_until_expiry !== undefined}
                  <span
                    class="badge badge-tag"
                    class:badge-block={tlsPingResult.days_until_expiry < 14}
                    class:badge-direct={tlsPingResult.days_until_expiry >= 14}
                    data-testid="tls-ping-days"
                  >
                    {tlsPingResult.days_until_expiry}
                  </span>
                {:else}
                  <span>—</span>
                {/if}
              </div>
            </div>
            {#if tlsPingResult.dns_names && tlsPingResult.dns_names.length > 0}
              <div class="tls-ping-dns">
                <span class="text-muted">{$t('xray.tls_ping.dns_names')}:</span>
                <span class="dns-value">{tlsPingResult.dns_names.join(', ')}</span>
              </div>
            {/if}
          {/if}
        </div>
      {/if}

      {#if outbound.security === 'reality'}
        <div class="form-row2">
          <div class="form-col">
            <label class="form-label" for="outbound-pubkey">
              {$t('xray.reality_public_key')}
            </label>
            <input
              id="outbound-pubkey"
              class="form-input"
              bind:value={outbound.publicKey}
              placeholder="base64"
            />
          </div>
          <div class="form-col">
            <label class="form-label" for="outbound-shortid">
              {$t('xray.reality_short_id')}
            </label>
            <input
              id="outbound-shortid"
              class="form-input"
              bind:value={outbound.shortId}
              placeholder="0123abcd"
            />
          </div>
        </div>
        <div class="form-row">
          <label class="form-label" for="outbound-fingerprint">
            {$t('xray.reality_fingerprint')}
          </label>
          <Select id="outbound-fingerprint" class="form-select" bind:value={outbound.fingerprint}>
            <option value="chrome">chrome</option>
            <option value="firefox">firefox</option>
            <option value="safari">safari</option>
            <option value="edge">edge</option>
            <option value="qq">qq</option>
          </Select>
        </div>
      {/if}
    {/if}

    <!-- Transport Settings -->
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="outbound-network">
          {$t('xray.network_transport')}
        </label>
        <Select id="outbound-network" class="form-select" bind:value={outbound.network}>
          <option value="tcp">tcp</option>
          <option value="ws">websocket (ws)</option>
          <option value="grpc">gRPC</option>
          <option value="xhttp">xhttp (SplitHTTP)</option>
        </Select>
      </div>
      <div class="form-col">
        {#if outbound.network === 'ws'}
          <label class="form-label" for="outbound-path">{$t('xray.ws_path')}</label>
          <input id="outbound-path" class="form-input" bind:value={outbound.path} placeholder="/" />
        {:else}
          <label class="form-label" for="outbound-service">
            {$t('xray.grpc_service_name')}
          </label>
          <input
            id="outbound-service"
            class="form-input"
            bind:value={outbound.serviceName}
            placeholder="grpc-service"
          />
        {/if}
      </div>
    </div>
  {:else if outbound.protocol === 'shadowsocks'}
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="outbound-ss-address">
          {$t('xray.server_address')} *
        </label>
        <input
          id="outbound-ss-address"
          class="form-input"
          bind:value={outbound.address}
          placeholder="server.com"
        />
      </div>
      <div class="form-col">
        <label class="form-label" for="outbound-ss-port">{$t('xray.port')} *</label>
        <input
          id="outbound-ss-port"
          class="form-input"
          type="number"
          bind:value={outbound.port}
          min="1"
          max="65535"
        />
      </div>
    </div>
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="outbound-ss-cipher">{$t('xray.cipher')}</label>
        <Select id="outbound-ss-cipher" class="form-select" bind:value={outbound.cipher}>
          {#each shadowsocksCiphers as c}
            <option value={c}>{c}</option>
          {/each}
        </Select>
      </div>
      <div class="form-col">
        <label class="form-label" for="outbound-ss-password">
          {$t('xray.password_key')} *
        </label>
        <div class="input-with-btn">
          <input
            id="outbound-ss-password"
            class="form-input"
            bind:value={outbound.shadowsocksPassword}
            placeholder="base64"
          />
          {#if outbound.cipher?.startsWith('2022-blake3')}
            <button
              class="btn btn-secondary btn-inset"
              onclick={() => {
                outbound.shadowsocksPassword = generateShadowsocksKey(outbound.cipher);
                showToast('success', $t('xray.key_generated'));
              }}
              title={$t('xray.generate_key')}
              aria-label={$t('xray.generate_key')}
              data-testid="outbound-ss-generate-key"
              type="button"
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                aria-hidden="true"
              >
                <polyline points="23 4 23 10 17 10" />
                <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10" />
              </svg>
            </button>
          {/if}
        </div>
      </div>
    </div>
  {:else if outbound.protocol === 'wireguard'}
    {#if outbound.isAwgObfuscated}
      <div class="alert alert-warning awg-obfuscation-alert">
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          aria-hidden="true"
        >
          <path
            d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"
          />
          <line x1="12" y1="9" x2="12" y2="13" />
          <line x1="12" y1="17" x2="12.01" y2="17" />
        </svg>
        <span>{$t('xray.awg_warning')}</span>
      </div>
      {@const diff = analyzeAwgDiff(
        outbound.rawAwgOptions || {
          jc: 4,
          jmin: 40,
          jmax: 70,
          s1: 15,
          s2: 40,
          h1: 1000000001,
          h2: 1000000002,
          h3: 1000000003,
          h4: 1000000004
        },
        'xray'
      )}
      <div style="margin-bottom: 12px;">
        <AwgDiffCard {diff} targetKernel="xray" compact />
      </div>
    {/if}
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="outbound-wg-endpoint">
          {$t('xray.endpoint')} *
        </label>
        <input
          id="outbound-wg-endpoint"
          class="form-input"
          bind:value={outbound.endpoint}
          placeholder="server.com:51820"
        />
      </div>
      <div class="form-col">
        <label class="form-label" for="outbound-wg-address">
          {$t('xray.local_address')}
        </label>
        <input
          id="outbound-wg-address"
          class="form-input"
          bind:value={outbound.wireguardAddress}
          placeholder="10.0.0.2/32, fd00::2/128"
        />
      </div>
    </div>
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="outbound-wg-secret-key">
          {$t('xray.private_key')} *
        </label>
        <input
          id="outbound-wg-secret-key"
          class="form-input"
          bind:value={outbound.wireguardSecretKey}
          placeholder="base64"
        />
      </div>
      <div class="form-col">
        <label class="form-label" for="outbound-wg-public-key">
          {$t('xray.public_key')} *
        </label>
        <input
          id="outbound-wg-public-key"
          class="form-input"
          bind:value={outbound.wireguardPublicKey}
          placeholder="base64"
        />
      </div>
    </div>
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="outbound-wg-psk">
          {$t('xray.preshared_key')}
        </label>
        <input
          id="outbound-wg-psk"
          class="form-input"
          bind:value={outbound.wireguardPsk}
          placeholder="base64"
        />
      </div>
      <div class="form-col">
        <label class="form-label" for="outbound-wg-allowed-ips">
          {$t('xray.allowed_ips')}
        </label>
        <input
          id="outbound-wg-allowed-ips"
          class="form-input"
          bind:value={outbound.wireguardAllowedIPs}
          placeholder="0.0.0.0/0, ::/0"
        />
      </div>
    </div>
    <div class="form-row3">
      <div class="form-col">
        <label class="form-label" for="outbound-wg-keepalive">
          {$t('xray.keepalive')}
        </label>
        <input
          id="outbound-wg-keepalive"
          class="form-input"
          type="number"
          bind:value={outbound.wireguardKeepAlive}
          placeholder="25"
        />
      </div>
      <div class="form-col">
        <label class="form-label" for="outbound-wg-mtu">{$t('xray.mtu')}</label>
        <input
          id="outbound-wg-mtu"
          class="form-input"
          type="number"
          bind:value={outbound.wireguardMtu}
          min="1200"
          max="1500"
          placeholder="1420"
        />
        {#if outbound.isAwgObfuscated}
          <div class="field-info-hint">
            {$t('proxies.awg_mtu_hint')}
          </div>
        {/if}
      </div>
      <div class="form-col">
        <label class="form-label" for="outbound-wg-reserved">
          {$t('xray.reserved')}
        </label>
        <input
          id="outbound-wg-reserved"
          class="form-input"
          bind:value={outbound.wireguardReserved}
          placeholder="1, 2, 3"
        />
      </div>
    </div>
  {/if}

  <!-- Sockopt Section -->
  <details class="sockopt-details">
    <summary class="sockopt-summary">
      <svg
        class="sockopt-summary-icon"
        width="14"
        height="14"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        aria-hidden="true"
      >
        <circle cx="12" cy="12" r="3" />
        <path
          d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"
        />
      </svg>
      <span>{$t('xray.sockopt_title')}</span>
    </summary>
    <div class="sockopt-body">
      <div class="form-row2">
        <div class="form-col">
          <label class="form-label" for="sockopt-mark">{$t('xray.mark')}</label>
          <input
            id="sockopt-mark"
            class="form-input"
            type="number"
            bind:value={outbound.sockoptMark}
            placeholder="e.g. 255"
          />
          <div class="form-hint">
            {$t('xray.sockopt_hint')}
          </div>
        </div>
        <div class="form-col">
          <label class="form-label" for="sockopt-keepalive">
            {$t('xray.tcp_keepalive_interval')}
          </label>
          <input
            id="sockopt-keepalive"
            class="form-input"
            type="number"
            bind:value={outbound.sockoptTcpKeepAliveInterval}
            placeholder="seconds"
          />
        </div>
      </div>
      <div class="sockopt-checks">
        <label class="checkbox-container">
          <input type="checkbox" bind:checked={outbound.sockoptTcpFastOpen} />
          <span class="checkmark"></span>
          <span>{$t('xray.tcp_fast_open')}</span>
        </label>
        <label class="checkbox-container">
          <input type="checkbox" bind:checked={outbound.sockoptTcpMptcp} />
          <span class="checkmark"></span>
          <span>{$t('xray.tcp_mptcp')}</span>
        </label>
        <label class="checkbox-container">
          <input type="checkbox" bind:checked={outbound.sockoptTcpNoDelay} />
          <span class="checkmark"></span>
          <span>{$t('xray.tcp_nodelay')}</span>
        </label>
      </div>
    </div>
  </details>

  <!-- Dialer Proxy Section -->
  <div class="form-row dialer-proxy-row">
    <label class="form-label" for="outbound-dialer-proxy">
      {$t('xray.dialer_proxy')}
    </label>
    <Select id="outbound-dialer-proxy" class="form-select" bind:value={outbound.dialerProxy}>
      <option value="">{$t('xray.dialer_none')}</option>
      {#each outboundDetails.filter((d) => !['direct', 'block', 'dns-out'].includes(d.tag) && d.tag !== (outbound.tag || '').trim()) as o}
        <option value={o.tag}>{o.tag} ({o.protocol})</option>
      {/each}
    </Select>

    {#if outbound.dialerProxy}
      <div class="dialer-chain-preview">
        <span class="text-muted">{$t('xray.dialer_chain')}:</span>
        {#each dialerChainPreview.chain as node, idx}
          <span
            class="badge"
            class:badge-primary={idx === 0}
            class:badge-secondary={idx > 0 && node !== 'DIRECT'}
            class:badge-tag={node === 'DIRECT'}
          >
            {node}
          </span>
          {#if idx < dialerChainPreview.chain.length - 1}
            <span class="dialer-chain-sep" aria-hidden="true">→</span>
          {/if}
        {/each}
        {#if dialerChainPreview.hasCycle}
          <span class="badge badge-danger">
            {$t('xray.dialer_cycle_detected')}
          </span>
        {/if}
      </div>
    {/if}
  </div>

  <div class="form-actions form-actions--sticky">
    <button class="btn btn-secondary" onclick={onCancel} type="button">
      {$t('app.cancel')}
    </button>
    <button class="btn btn-primary" onclick={onSave} type="button">
      {$t(isEdit ? 'app.save' : 'app.add')}
    </button>
  </div>
</div>

<style>
  .xray-outbound-form {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .form-row {
    margin-bottom: 0;
  }

  .form-row2 {
    display: flex;
    gap: 12px;
  }

  .form-row3 {
    display: flex;
    gap: 12px;
  }

  .form-col {
    flex: 1;
    min-width: 0;
  }

  .form-label {
    display: block;
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--fg-secondary);
    margin-bottom: 4px;
  }

  .form-input {
    width: 100%;
    box-sizing: border-box;
    padding: 6px 10px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: 0.8125rem;
    outline: none;
    transition:
      border-color var(--transition-fast),
      background-color var(--transition-fast);
  }

  .form-input:focus {
    border-color: var(--accent);
    background: var(--bg-card);
  }

  .input-with-btn {
    display: flex;
    position: relative;
    align-items: center;
  }

  .btn-inset {
    position: absolute;
    right: 4px;
    height: 24px;
    padding: 0 8px;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .security-tools-row {
    margin-bottom: 4px;
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    flex-wrap: wrap;
  }

  .tls-ping-result {
    padding: 12px;
    background: var(--bg-page);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }

  .tls-ping-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
  }

  .tls-ping-title {
    font-size: 0.8125rem;
    font-weight: 600;
    color: var(--fg-primary);
  }

  .tls-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
    gap: 8px;
    font-size: 0.75rem;
  }

  .tls-ping-dns {
    margin-top: 8px;
    font-size: 0.75rem;
  }

  .dns-value {
    color: var(--fg-primary);
    word-break: break-all;
  }

  .sockopt-details {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .sockopt-summary {
    padding: 10px var(--spacing-3);
    cursor: pointer;
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--fg-primary);
    display: flex;
    align-items: center;
    gap: 8px;
    user-select: none;
  }

  .sockopt-body {
    padding: var(--spacing-3);
    border-top: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .sockopt-checks {
    display: flex;
    gap: 16px;
    flex-wrap: wrap;
  }

  .checkbox-container {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    cursor: pointer;
    font-size: 0.75rem;
    color: var(--fg-secondary);
    user-select: none;
  }

  .dialer-proxy-row {
    margin-top: 4px;
  }

  .dialer-chain-preview {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    margin-top: 8px;
    font-size: 0.75rem;
  }

  .dialer-chain-sep {
    color: var(--fg-muted);
  }

  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 16px;
  }

  .form-actions--sticky {
    position: sticky;
    bottom: 0;
    background: var(--bg-card);
    padding-top: 12px;
    border-top: 1px solid var(--border);
    z-index: 5;
  }

  @media (max-width: 600px) {
    .form-row2,
    .form-row3 {
      flex-direction: column;
      gap: 8px;
    }
  }
</style>
