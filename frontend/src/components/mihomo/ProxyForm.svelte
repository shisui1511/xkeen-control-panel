<script lang="ts">
  import { t } from '../../i18n';
  import { capabilities } from '../../stores';
  import { isMihomoAwg31Supported } from '../../lib/awgFields';
  import {
    AWG_PRESETS,
    getAwgPreset,
    generateRandomAwgParams,
    type AwgPresetId
  } from '../../lib/awgPresets';

  let {
    np = $bindable(),
    onSave,
    onCancel,
    isEdit = false,
    isImported = false,
    allProxyNames = []
  }: {
    np: any;
    onSave: () => void;
    onCancel: () => void;
    isEdit?: boolean;
    isImported?: boolean;
    allProxyNames?: string[];
  } = $props();

  let showAllFields = $state(false);
  const isImportedCompact = $derived(isEdit && isImported && !showAllFields);

  const availableDialers = $derived(
    (allProxyNames || []).filter((n: string) => n && n !== np.name)
  );

  const dialerChain = $derived.by(() => {
    const currentName = np.name || 'node';
    if (!np.dialerProxy) {
      return { chain: [currentName, 'DIRECT'], hasCycle: false };
    }
    if (np.dialerProxy === currentName) {
      return { chain: [currentName, np.dialerProxy], hasCycle: true };
    }
    return { chain: [currentName, np.dialerProxy, 'DIRECT'], hasCycle: false };
  });

  const PROXY_TYPES = [
    'vless',
    'hysteria2',
    'tuic',
    'ss',
    'vmess',
    'trojan',
    'wireguard',
    'socks5',
    'http'
  ];
  const CIPHERS = ['aes-256-gcm', 'aes-128-gcm', 'chacha20-poly1305', '2022-blake3-aes-256-gcm'];

  const mihomoVersion = $derived($capabilities?.kernels?.mihomo?.version || '');
  const activeKernel = $derived($capabilities?.active_kernel || 'mihomo');
  const isAwg31Allowed = $derived(activeKernel !== 'xray' && isMihomoAwg31Supported(mihomoVersion));

  // Ограничения AmneziaWG (TMPL-08, AWG-01..05, AWGVAL-01..05):
  type HParsed = { ok: boolean; min: number; max: number; raw: string };

  function parseH(val: any): HParsed {
    if (val === undefined || val === null || val === '') {
      return { ok: false, min: 0, max: 0, raw: '' };
    }
    const s = String(val).trim();
    if (s.includes('-')) {
      const parts = s.split('-').map((p) => parseInt(p.trim(), 10));
      if (
        parts.length === 2 &&
        !isNaN(parts[0]) &&
        !isNaN(parts[1]) &&
        parts[0] > 4 &&
        parts[1] >= parts[0]
      ) {
        return { ok: true, min: parts[0], max: parts[1], raw: s };
      }
      return { ok: false, min: 0, max: 0, raw: s };
    }
    const n = parseInt(s, 10);
    if (!isNaN(n) && n > 4) {
      return { ok: true, min: n, max: n, raw: s };
    }
    return { ok: false, min: 0, max: 0, raw: s };
  }

  function rangesOverlap(a: HParsed, b: HParsed): boolean {
    if (!a.ok || !b.ok) return false;
    return a.max >= b.min && a.min <= b.max;
  }

  const parsedH1 = $derived(parseH(np.awgH1));
  const parsedH2 = $derived(parseH(np.awgH2));
  const parsedH3 = $derived(parseH(np.awgH3));
  const parsedH4 = $derived(parseH(np.awgH4));

  const isH1Invalid = $derived(
    np.awgEnabled &&
      (!parsedH1.ok ||
        rangesOverlap(parsedH1, parsedH2) ||
        rangesOverlap(parsedH1, parsedH3) ||
        rangesOverlap(parsedH1, parsedH4))
  );
  const isH2Invalid = $derived(
    np.awgEnabled &&
      (!parsedH2.ok ||
        rangesOverlap(parsedH2, parsedH1) ||
        rangesOverlap(parsedH2, parsedH3) ||
        rangesOverlap(parsedH2, parsedH4))
  );
  const isH3Invalid = $derived(
    np.awgEnabled &&
      (!parsedH3.ok ||
        rangesOverlap(parsedH3, parsedH1) ||
        rangesOverlap(parsedH3, parsedH2) ||
        rangesOverlap(parsedH3, parsedH4))
  );
  const isH4Invalid = $derived(
    np.awgEnabled &&
      (!parsedH4.ok ||
        rangesOverlap(parsedH4, parsedH1) ||
        rangesOverlap(parsedH4, parsedH2) ||
        rangesOverlap(parsedH4, parsedH3))
  );

  const isJInvalid = $derived(np.awgEnabled && Number(np.awgJmin) >= Number(np.awgJmax));
  const isSInvalid = $derived(np.awgEnabled && Number(np.awgS1) + 56 === Number(np.awgS2));

  const mtuVal = $derived(Number(np.wgMtu) || 1420);
  const isJunkMtuInvalid = $derived(np.awgEnabled && Number(np.awgJmax) + 80 > mtuVal);

  const hasHeaderProtection = $derived(
    np.awgEnabled &&
      typeof np.awgHeaderProtectionKey === 'string' &&
      np.awgHeaderProtectionKey.trim() !== ''
  );

  const isSHeaderProtectionInvalid = $derived.by(() => {
    if (!hasHeaderProtection) return false;
    if (Number(np.awgS1) < 12 || Number(np.awgS2) < 12) return true;
    if (np.awgS3 !== undefined && np.awgS3 !== null && np.awgS3 !== '' && Number(np.awgS3) < 12) {
      return true;
    }
    if (np.awgS4 !== undefined && np.awgS4 !== null && np.awgS4 !== '' && Number(np.awgS4) < 12) {
      return true;
    }
    return false;
  });

  const awgConstraintsOk = $derived(
    !np.awgEnabled ||
      (!isJInvalid &&
        !isSInvalid &&
        !isH1Invalid &&
        !isH2Invalid &&
        !isH3Invalid &&
        !isH4Invalid &&
        !isSHeaderProtectionInvalid &&
        !isJunkMtuInvalid)
  );

  let selectedPreset = $state<string>('');

  function handleApplyPreset(presetId: string) {
    selectedPreset = presetId;
    if (!presetId) return;
    const preset = getAwgPreset(presetId as AwgPresetId, mihomoVersion);
    if (preset) {
      if (preset.jc !== undefined) np.awgJc = preset.jc;
      if (preset.jmin !== undefined) np.awgJmin = preset.jmin;
      if (preset.jmax !== undefined) np.awgJmax = preset.jmax;
      if (preset.s1 !== undefined) np.awgS1 = preset.s1;
      if (preset.s2 !== undefined) np.awgS2 = preset.s2;
      if (preset.s3 !== undefined) np.awgS3 = preset.s3;
      if (preset.s4 !== undefined) np.awgS4 = preset.s4;
      if (preset.h1 !== undefined) np.awgH1 = preset.h1;
      if (preset.h2 !== undefined) np.awgH2 = preset.h2;
      if (preset.h3 !== undefined) np.awgH3 = preset.h3;
      if (preset.h4 !== undefined) np.awgH4 = preset.h4;
      if (preset.version !== undefined && isAwg31Allowed) np.awgVersion = preset.version;
      if (preset.headerProtectionKey !== undefined && isAwg31Allowed)
        np.awgHeaderProtectionKey = preset.headerProtectionKey;
      if (preset.i1 !== undefined && isAwg31Allowed) np.awgI1 = preset.i1;
      if (preset.i2 !== undefined && isAwg31Allowed) np.awgI2 = preset.i2;
      if (preset.i3 !== undefined && isAwg31Allowed) np.awgI3 = preset.i3;
      if (preset.i4 !== undefined && isAwg31Allowed) np.awgI4 = preset.i4;
      if (preset.i5 !== undefined && isAwg31Allowed) np.awgI5 = preset.i5;
      if (preset.mtu !== undefined) np.wgMtu = preset.mtu;
      else if (!np.wgMtu || np.wgMtu > 1280) np.wgMtu = 1280;
    }
  }

  function handleRegenerateAwg() {
    selectedPreset = '';
    const rand = generateRandomAwgParams(mihomoVersion);
    np.awgJc = rand.jc;
    np.awgJmin = rand.jmin;
    np.awgJmax = rand.jmax;
    np.awgS1 = rand.s1;
    np.awgS2 = rand.s2;
    np.awgS3 = rand.s3;
    np.awgS4 = rand.s4;
    np.awgH1 = rand.h1;
    np.awgH2 = rand.h2;
    np.awgH3 = rand.h3;
    np.awgH4 = rand.h4;
    if (isAwg31Allowed) {
      if (rand.version) np.awgVersion = rand.version;
      if (rand.headerProtectionKey) np.awgHeaderProtectionKey = rand.headerProtectionKey;
      if (rand.i1) np.awgI1 = rand.i1;
      if (rand.i2) np.awgI2 = rand.i2;
      if (rand.i3) np.awgI3 = rand.i3;
      if (rand.i4) np.awgI4 = rand.i4;
      if (rand.i5) np.awgI5 = rand.i5;
    }
    if (!np.wgMtu || np.wgMtu > 1280) {
      np.wgMtu = 1280;
    }
  }

  function handleToggleAwg(e: Event) {
    const target = e.target as HTMLInputElement;
    if (target.checked) {
      if (!np.wgMtu || np.wgMtu > 1280) {
        np.wgMtu = 1280;
      }
    }
  }
</script>

<div class="form-card">
  {#snippet awgBlock()}
    <div class="awg-options-block" class:has-warning={!awgConstraintsOk}>
      <!-- ПАНЕЛЬ ПРЕСЕТОВ ОБФУСКАЦИИ И РАНДОМИЗАЦИИ (AWGUX-01, AWGUX-02) -->
      <div class="awg-preset-bar">
        <div class="preset-select-wrap">
          <label class="form-label" for="proxy-awg-preset">{$t('proxies.awg_preset')}</label>
          <select
            id="proxy-awg-preset"
            class="form-select"
            value={selectedPreset}
            onchange={(e) => handleApplyPreset((e.target as HTMLSelectElement).value)}
          >
            <option value="">{$t('proxies.awg_preset_custom')}</option>
            <option value="standard">{$t('proxies.awg_preset_standard')}</option>
            <option value="mobile-lte">{$t('proxies.awg_preset_mobile_lte')}</option>
            <option value="aggressive-dpi">{$t('proxies.awg_preset_aggressive_dpi')}</option>
            <option value="compatibility">{$t('proxies.awg_preset_compatibility')}</option>
            <option value="low-ram-mips">{$t('proxies.awg_preset_low_ram_mips')}</option>
          </select>
        </div>
        <button
          type="button"
          class="btn btn-secondary btn-sm regenerate-btn"
          onclick={handleRegenerateAwg}
          title={$t('proxies.awg_regenerate_hint')}
        >
          🎲 {$t('proxies.awg_regenerate')}
        </button>
      </div>

      <!-- СЕКЦИЯ 1: КЛАССИЧЕСКИЙ AWG 1.0 -->
      <div class="awg-subgroup">
        <div class="awg-group-title">{$t('proxies.awg_section_classic')}</div>

        <div class="form-row">
          <div class="form-label">{$t('proxies.awg_junk')}</div>
          <div class="form-row2" style="grid-template-columns: repeat(3, 1fr); gap: 8px;">
            <div class="form-col">
              <label class="form-label" for="proxy-awg-jc">Jc</label>
              <input
                id="proxy-awg-jc"
                class="form-input"
                type="number"
                bind:value={np.awgJc}
                min="1"
                max="128"
              />
            </div>
            <div class="form-col">
              <label class="form-label" for="proxy-awg-jmin">Jmin</label>
              <input
                id="proxy-awg-jmin"
                class="form-input"
                class:input-warning={isJInvalid}
                aria-invalid={isJInvalid}
                type="number"
                bind:value={np.awgJmin}
                min="0"
              />
            </div>
            <div class="form-col">
              <label class="form-label" for="proxy-awg-jmax">Jmax</label>
              <input
                id="proxy-awg-jmax"
                class="form-input"
                class:input-warning={isJInvalid || isJunkMtuInvalid}
                aria-invalid={isJInvalid || isJunkMtuInvalid}
                type="number"
                bind:value={np.awgJmax}
                min="0"
              />
            </div>
          </div>
          {#if isJInvalid}
            <div class="field-error-hint">
              {$t('preflight.awg_jmin_jmax', { jmin: np.awgJmin, jmax: np.awgJmax })}
            </div>
          {/if}
          {#if isJunkMtuInvalid}
            <div class="field-error-hint">
              {$t('proxies.awg_junk_mtu_warning', { mtu: mtuVal })}
            </div>
          {/if}
        </div>

        <div class="form-row">
          <div class="form-label">{$t('proxies.awg_padding')}</div>
          <div class="form-row2" style="grid-template-columns: 1fr 1fr; gap: 8px;">
            <div class="form-col">
              <label class="form-label" for="proxy-awg-s1">S1</label>
              <input
                id="proxy-awg-s1"
                class="form-input"
                class:input-warning={isSInvalid || (hasHeaderProtection && Number(np.awgS1) < 12)}
                aria-invalid={isSInvalid || (hasHeaderProtection && Number(np.awgS1) < 12)}
                type="number"
                bind:value={np.awgS1}
                min="0"
              />
            </div>
            <div class="form-col">
              <label class="form-label" for="proxy-awg-s2">S2</label>
              <input
                id="proxy-awg-s2"
                class="form-input"
                class:input-warning={isSInvalid || (hasHeaderProtection && Number(np.awgS2) < 12)}
                aria-invalid={isSInvalid || (hasHeaderProtection && Number(np.awgS2) < 12)}
                type="number"
                bind:value={np.awgS2}
                min="0"
              />
            </div>
          </div>
          {#if isSInvalid}
            <div class="field-error-hint">{$t('preflight.awg_s1_s2')}</div>
          {/if}
        </div>

        <div class="form-row">
          <div class="form-label">{$t('proxies.awg_headers')}</div>
          <div class="form-row2 awg-headers-grid">
            <div class="form-col">
              <label class="form-label" for="proxy-awg-h1">H1</label>
              <input
                id="proxy-awg-h1"
                class="form-input"
                class:input-warning={isH1Invalid}
                aria-invalid={isH1Invalid}
                type="text"
                bind:value={np.awgH1}
                placeholder="1000000001 or min-max"
              />
            </div>
            <div class="form-col">
              <label class="form-label" for="proxy-awg-h2">H2</label>
              <input
                id="proxy-awg-h2"
                class="form-input"
                class:input-warning={isH2Invalid}
                aria-invalid={isH2Invalid}
                type="text"
                bind:value={np.awgH2}
                placeholder="1000000002 or min-max"
              />
            </div>
            <div class="form-col">
              <label class="form-label" for="proxy-awg-h3">H3</label>
              <input
                id="proxy-awg-h3"
                class="form-input"
                class:input-warning={isH3Invalid}
                aria-invalid={isH3Invalid}
                type="text"
                bind:value={np.awgH3}
                placeholder="1000000003 or min-max"
              />
            </div>
            <div class="form-col">
              <label class="form-label" for="proxy-awg-h4">H4</label>
              <input
                id="proxy-awg-h4"
                class="form-input"
                class:input-warning={isH4Invalid}
                aria-invalid={isH4Invalid}
                type="text"
                bind:value={np.awgH4}
                placeholder="1000000004 or min-max"
              />
            </div>
          </div>
          {#if isH1Invalid || isH2Invalid || isH3Invalid || isH4Invalid}
            <div class="field-error-hint">{$t('preflight.awg_h_unique')}</div>
          {/if}
        </div>
      </div>

      <!-- СЕКЦИЯ 2: AMNEZIAWG 2.0 -->
      <div class="awg-subgroup">
        <div class="awg-group-title">{$t('proxies.awg_section_20')}</div>
        <div class="form-row2" style="grid-template-columns: 1fr 1fr; gap: 8px;">
          <div class="form-col">
            <label class="form-label" for="proxy-awg-s3">{$t('proxies.awg_s3')}</label>
            <input
              id="proxy-awg-s3"
              class="form-input"
              class:input-warning={hasHeaderProtection &&
                np.awgS3 !== undefined &&
                np.awgS3 !== null &&
                np.awgS3 !== '' &&
                Number(np.awgS3) < 12}
              type="number"
              bind:value={np.awgS3}
              min="0"
              placeholder="optional"
            />
          </div>
          <div class="form-col">
            <label class="form-label" for="proxy-awg-s4">{$t('proxies.awg_s4')}</label>
            <input
              id="proxy-awg-s4"
              class="form-input"
              class:input-warning={hasHeaderProtection &&
                np.awgS4 !== undefined &&
                np.awgS4 !== null &&
                np.awgS4 !== '' &&
                Number(np.awgS4) < 12}
              type="number"
              bind:value={np.awgS4}
              min="0"
              placeholder="optional"
            />
          </div>
        </div>
      </div>

      <!-- СЕКЦИЯ 3: AMNEZIAWG 3.1 -->
      <div class="awg-subgroup">
        <div class="awg-group-title">{$t('proxies.awg_section_31')}</div>

        {#if !isAwg31Allowed}
          <div class="awg-warning-banner">
            <span>⚠️</span>
            <span>{$t('proxies.awg_kernel_incompatible_hint')}</span>
          </div>
        {/if}

        {#if isSHeaderProtectionInvalid}
          <div class="field-error-hint">{$t('proxies.awg_s_header_protection_warning')}</div>
        {/if}

        <div class="form-row2">
          <div class="form-col">
            <label class="form-label" for="proxy-awg-version">{$t('proxies.awg_version')}</label>
            <input
              id="proxy-awg-version"
              class="form-input"
              type="text"
              bind:value={np.awgVersion}
              disabled={!isAwg31Allowed}
              placeholder="3.1"
            />
          </div>
          <div class="form-col">
            <label class="form-label" for="proxy-awg-header-protection-key"
              >{$t('proxies.awg_header_protection_key')}</label
            >
            <input
              id="proxy-awg-header-protection-key"
              class="form-input"
              type="text"
              bind:value={np.awgHeaderProtectionKey}
              disabled={!isAwg31Allowed}
              placeholder="key string or hex"
            />
          </div>
        </div>

        <div class="form-row">
          <div class="form-label">{$t('proxies.awg_i_chain')}</div>
          <div class="awg-tokens-grid">
            <div class="form-col">
              <label class="form-label" for="proxy-awg-i1">I1</label>
              <input
                id="proxy-awg-i1"
                class="form-input"
                type="text"
                bind:value={np.awgI1}
                disabled={!isAwg31Allowed}
                placeholder="0x01 or <b 0xf1a0><c>"
              />
            </div>
            <div class="form-col">
              <label class="form-label" for="proxy-awg-i2">I2</label>
              <input
                id="proxy-awg-i2"
                class="form-input"
                type="text"
                bind:value={np.awgI2}
                disabled={!isAwg31Allowed}
                placeholder="0x02"
              />
            </div>
            <div class="form-col">
              <label class="form-label" for="proxy-awg-i3">I3</label>
              <input
                id="proxy-awg-i3"
                class="form-input"
                type="text"
                bind:value={np.awgI3}
                disabled={!isAwg31Allowed}
                placeholder="0x03"
              />
            </div>
            <div class="form-col">
              <label class="form-label" for="proxy-awg-i4">I4</label>
              <input
                id="proxy-awg-i4"
                class="form-input"
                type="text"
                bind:value={np.awgI4}
                disabled={!isAwg31Allowed}
                placeholder="0x04"
              />
            </div>
            <div class="form-col">
              <label class="form-label" for="proxy-awg-i5">I5</label>
              <input
                id="proxy-awg-i5"
                class="form-input"
                type="text"
                bind:value={np.awgI5}
                disabled={!isAwg31Allowed}
                placeholder="0x05"
              />
            </div>
          </div>
        </div>

        <div class="form-row2">
          <div class="form-col">
            <label class="form-label" for="proxy-awg-cpa"
              >{$t('proxies.awg_content_padding_addition')}</label
            >
            <input
              id="proxy-awg-cpa"
              class="form-input"
              type="number"
              bind:value={np.awgContentPaddingAddition}
              disabled={!isAwg31Allowed}
              min="0"
              placeholder="0-255"
            />
          </div>
          <div class="form-col">
            <label class="form-label" for="proxy-awg-rekey"
              >{$t('proxies.awg_rekey_after_time')}</label
            >
            <input
              id="proxy-awg-rekey"
              class="form-input"
              type="number"
              bind:value={np.awgRekeyAfterTime}
              disabled={!isAwg31Allowed}
              min="0"
              placeholder="seconds"
            />
          </div>
        </div>

        <div class="form-row2" style="gap: 16px; margin-top: 4px;">
          <label
            class="toggle-label"
            style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
          >
            <input type="checkbox" bind:checked={np.awgRandomTrailers} disabled={!isAwg31Allowed} />
            <span>{$t('proxies.awg_random_trailers')}</span>
          </label>
          <label
            class="toggle-label"
            style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
          >
            <input type="checkbox" bind:checked={np.awgDisableCookies} disabled={!isAwg31Allowed} />
            <span>{$t('proxies.awg_disable_cookies')}</span>
          </label>
        </div>
      </div>

      <p class="form-hint" style="margin-top: 4px;">
        {$t('proxies.awg_hint')}
      </p>
    </div>
  {/snippet}

  {#if isImportedCompact}
    <div class="imported-identity-box">
      <div class="identity-badge-row">
        <span class="item-badge type-{np.type}">{np.type}</span>
        <span class="mono-endpoint">{np.server || '0.0.0.0'}:{np.port || 0}</span>
        {#if np.uuid}
          <span class="mono-uuid" title={np.uuid}>UUID: {String(np.uuid).slice(0, 8)}...</span>
        {/if}
        <button type="button" class="toggle-all-fields-btn" onclick={() => (showAllFields = true)}>
          {$t('proxies.show_all_fields')}
        </button>
      </div>
      <div class="identity-hint">
        {$t('proxies.imported_readonly_hint')}
      </div>
    </div>

    <div class="form-row">
      <label class="form-label" for="proxy-name-compact">{$t('subscr.name')}</label>
      <input
        id="proxy-name-compact"
        class="form-input"
        bind:value={np.name}
        placeholder="my-proxy"
      />
    </div>

    <!-- Dialer Proxy Selector -->
    <div class="form-row">
      <label class="form-label" for="proxy-dialer-compact">{$t('proxies.dialer_proxy')}</label>
      <select id="proxy-dialer-compact" class="form-select" bind:value={np.dialerProxy}>
        <option value="">{$t('proxies.dialer_none')}</option>
        {#each availableDialers as d}
          <option value={d}>{d}</option>
        {/each}
      </select>
      {#if np.dialerProxy}
        <div class="dialer-chain-preview">
          <span class="chain-title">{$t('proxies.dialer_chain')}:</span>
          {#each dialerChain.chain as node, idx}
            <span
              class="badge"
              class:badge-primary={idx === 0}
              class:badge-tag={node === 'DIRECT'}
              class:badge-secondary={idx > 0 && node !== 'DIRECT'}
            >
              {node}
            </span>
            {#if idx < dialerChain.chain.length - 1}
              <span class="chain-arrow">→</span>
            {/if}
          {/each}
          {#if dialerChain.hasCycle}
            <span class="badge badge-danger">
              {$t('proxies.dialer_cycle_detected')}
            </span>
          {/if}
        </div>
      {/if}
    </div>

    {#if np.type === 'wireguard'}
      <div class="form-row">
        <label class="form-label" for="proxy-wg-mtu-compact">{$t('proxies.awg_mtu')}</label>
        <input
          id="proxy-wg-mtu-compact"
          class="form-input"
          type="number"
          bind:value={np.wgMtu}
          min="1200"
          max="1500"
          placeholder="1280"
        />
        {#if np.awgEnabled}
          <div class="form-hint" style="margin-top: 4px; font-size: 11px;">
            {$t('proxies.awg_mtu_hint')}
          </div>
        {/if}
      </div>

      <div class="form-row">
        <label
          class="toggle-label"
          style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
        >
          <input
            type="checkbox"
            id="proxy-awg-enabled-compact"
            bind:checked={np.awgEnabled}
            onchange={handleToggleAwg}
          />
          <span>{$t('proxies.awg_section')}</span>
        </label>
      </div>

      {#if np.awgEnabled}
        {@render awgBlock()}
      {/if}
    {/if}
  {:else}
    {#if isEdit && isImported}
      <div style="display: flex; justify-content: flex-end; margin-bottom: 8px;">
        <button type="button" class="toggle-all-fields-btn" onclick={() => (showAllFields = false)}>
          {$t('proxies.hide_all_fields')}
        </button>
      </div>
    {/if}
    <div class="form-row">
      <label class="form-label" for="proxy-type">{$t('proxies.type')}</label>
      <select id="proxy-type" class="form-select" bind:value={np.type}>
        {#each PROXY_TYPES as pt}<option value={pt}>{pt}</option>{/each}
      </select>
    </div>
    <div class="form-row">
      <label class="form-label" for="proxy-name">{$t('subscr.name')}</label>
      <input id="proxy-name" class="form-input" bind:value={np.name} placeholder="my-proxy" />
    </div>
    <div class="form-row2">
      <div class="form-col">
        <label class="form-label" for="proxy-server">{$t('proxies.server')}</label>
        <input
          id="proxy-server"
          class="form-input"
          bind:value={np.server}
          placeholder="example.com"
        />
      </div>
      <div class="form-col form-col-sm">
        <label class="form-label" for="proxy-port">{$t('proxies.port')}</label>
        <input
          id="proxy-port"
          class="form-input"
          type="number"
          bind:value={np.port}
          min="1"
          max="65535"
        />
      </div>
    </div>

    {#if np.type === 'vless'}
      <div class="form-row">
        <label class="form-label" for="proxy-vless-uuid">UUID</label>
        <div class="input-with-btn">
          <input id="proxy-vless-uuid" class="form-input" bind:value={np.uuid} placeholder="uuid" />
          <button
            class="btn-gen"
            onclick={() => (np.uuid = crypto.randomUUID())}
            title={$t('app.generate')}>⟳</button
          >
        </div>
      </div>
      <div class="form-row">
        <label class="form-label" for="proxy-vless-public-key">Reality Public Key</label>
        <input
          id="proxy-vless-public-key"
          class="form-input"
          bind:value={np.publicKey}
          placeholder="public-key"
        />
      </div>
      <div class="form-row2">
        <div class="form-col">
          <label class="form-label" for="proxy-vless-short-id">Short ID</label>
          <input
            id="proxy-vless-short-id"
            class="form-input"
            bind:value={np.shortId}
            placeholder="short-id"
          />
        </div>
        <div class="form-col">
          <label class="form-label" for="proxy-vless-sni">SNI</label>
          <input
            id="proxy-vless-sni"
            class="form-input"
            bind:value={np.servername}
            placeholder="www.apple.com"
          />
        </div>
      </div>
    {:else if np.type === 'hysteria2'}
      <div class="form-row">
        <label class="form-label" for="proxy-hysteria2-password">{$t('proxies.password')}</label>
        <input
          id="proxy-hysteria2-password"
          class="form-input"
          bind:value={np.password}
          placeholder="password"
        />
      </div>
      <div class="form-row">
        <label class="form-label" for="proxy-hysteria2-sni">SNI</label>
        <input
          id="proxy-hysteria2-sni"
          class="form-input"
          bind:value={np.sni}
          placeholder="example.com"
        />
      </div>
      <div class="form-row">
        <label class="form-label" for="proxy-hysteria2-obfs-type">{$t('editor.obfsType')}</label>
        <select id="proxy-hysteria2-obfs-type" class="form-select" bind:value={np.obfsType}>
          <option value="none">{$t('editor.none')}</option>
          <option value="simple">{$t('editor.simple')}</option>
        </select>
      </div>
      {#if np.obfsType === 'simple'}
        <div class="form-row">
          <label class="form-label" for="proxy-hysteria2-obfs-password"
            >{$t('editor.obfsPassword')}</label
          >
          <input
            id="proxy-hysteria2-obfs-password"
            class="form-input"
            bind:value={np.obfsPassword}
            placeholder="obfs password"
          />
        </div>
      {/if}
      <div class="form-row">
        <label
          class="toggle-label"
          style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
        >
          <input type="checkbox" bind:checked={np.skipCertVerify} />
          <span>{$t('editor.skipCertVerify')}</span>
        </label>
      </div>
    {:else if np.type === 'tuic'}
      <div class="form-row">
        <label class="form-label" for="proxy-tuic-uuid">UUID</label>
        <div class="input-with-btn">
          <input id="proxy-tuic-uuid" class="form-input" bind:value={np.uuid} placeholder="uuid" />
          <button class="btn-gen" onclick={() => (np.uuid = crypto.randomUUID())} title="Generate"
            >⟳</button
          >
        </div>
      </div>
      <div class="form-row">
        <label class="form-label" for="proxy-tuic-password">{$t('proxies.password')}</label>
        <input
          id="proxy-tuic-password"
          class="form-input"
          bind:value={np.password}
          placeholder="password"
        />
      </div>
      <div class="form-row">
        <label class="form-label" for="proxy-tuic-sni">SNI</label>
        <input
          id="proxy-tuic-sni"
          class="form-input"
          bind:value={np.sni}
          placeholder="example.com"
        />
      </div>
    {:else if np.type === 'ss'}
      <div class="form-row">
        <label class="form-label" for="proxy-ss-cipher">Cipher</label>
        <select id="proxy-ss-cipher" class="form-select" bind:value={np.cipher}>
          {#each CIPHERS as c}<option value={c}>{c}</option>{/each}
        </select>
      </div>
      <div class="form-row">
        <label class="form-label" for="proxy-ss-password">{$t('proxies.password')}</label>
        <input
          id="proxy-ss-password"
          class="form-input"
          bind:value={np.password}
          placeholder="password"
        />
      </div>
    {:else if np.type === 'vmess'}
      <div class="form-row">
        <label class="form-label" for="proxy-vmess-uuid">UUID</label>
        <div class="input-with-btn">
          <input id="proxy-vmess-uuid" class="form-input" bind:value={np.uuid} placeholder="uuid" />
          <button class="btn-gen" onclick={() => (np.uuid = crypto.randomUUID())} title="Generate"
            >⟳</button
          >
        </div>
      </div>
      <div class="form-row2">
        <div class="form-col">
          <label class="form-label" for="proxy-vmess-network">Network</label>
          <select id="proxy-vmess-network" class="form-select" bind:value={np.network}>
            <option value="ws">WebSocket</option>
            <option value="tcp">TCP</option>
            <option value="grpc">gRPC</option>
          </select>
        </div>
        <div class="form-col">
          <label class="form-label" for="proxy-vmess-tls">TLS</label>
          <input
            id="proxy-vmess-tls"
            type="checkbox"
            bind:checked={np.tls}
            style="margin-top:8px"
          />
        </div>
      </div>
      {#if np.network === 'ws'}
        <div class="form-row">
          <label class="form-label" for="proxy-vmess-ws-path">WS Path</label>
          <input
            id="proxy-vmess-ws-path"
            class="form-input"
            bind:value={np.wsPath}
            placeholder="/"
          />
        </div>
      {/if}
      {#if np.tls}
        <div class="form-row">
          <label class="form-label" for="proxy-vmess-sni">SNI</label>
          <input
            id="proxy-vmess-sni"
            class="form-input"
            bind:value={np.sni}
            placeholder="example.com"
          />
        </div>
      {/if}
    {:else if np.type === 'trojan'}
      <div class="form-row">
        <label class="form-label" for="proxy-trojan-password">{$t('proxies.password')}</label>
        <input
          id="proxy-trojan-password"
          class="form-input"
          bind:value={np.password}
          placeholder="password"
        />
      </div>
      <div class="form-row">
        <label class="form-label" for="proxy-trojan-sni">SNI</label>
        <input
          id="proxy-trojan-sni"
          class="form-input"
          bind:value={np.sni}
          placeholder="example.com"
        />
      </div>
      <div class="form-row2">
        <div class="form-col">
          <label class="form-label" for="proxy-trojan-network">Network</label>
          <select id="proxy-trojan-network" class="form-select" bind:value={np.network}>
            <option value="tcp">TCP</option>
            <option value="ws">WebSocket</option>
          </select>
        </div>
      </div>
      {#if np.network === 'ws'}
        <div class="form-row">
          <label class="form-label" for="proxy-trojan-ws-path">WS Path</label>
          <input
            id="proxy-trojan-ws-path"
            class="form-input"
            bind:value={np.wsPath}
            placeholder="/"
          />
        </div>
      {/if}
      <div class="form-row">
        <label
          class="toggle-label"
          style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
        >
          <input type="checkbox" bind:checked={np.skipCertVerify} />
          <span>{$t('editor.skipCertVerify')}</span>
        </label>
      </div>
    {:else if np.type === 'wireguard'}
      <div class="form-row">
        <label class="form-label" for="proxy-wg-private-key">{$t('proxies.wg_private_key')}</label>
        <input
          id="proxy-wg-private-key"
          class="form-input"
          bind:value={np.wgPrivateKey}
          placeholder="private-key"
        />
      </div>
      <div class="form-row">
        <label class="form-label" for="proxy-wg-public-key">{$t('proxies.wg_public_key')}</label>
        <input
          id="proxy-wg-public-key"
          class="form-input"
          bind:value={np.wgPublicKey}
          placeholder="public-key"
        />
      </div>
      <div class="form-row2">
        <div class="form-col">
          <label class="form-label" for="proxy-wg-ip">{$t('proxies.wg_ip')}</label>
          <input
            id="proxy-wg-ip"
            class="form-input"
            bind:value={np.wgIp}
            placeholder="10.2.0.2/32"
          />
        </div>
        <div class="form-col form-col-sm">
          <label class="form-label" for="proxy-wg-mtu">MTU</label>
          <input
            id="proxy-wg-mtu"
            class="form-input"
            type="number"
            bind:value={np.wgMtu}
            min="1200"
            max="1500"
            placeholder="1280"
          />
          {#if np.awgEnabled}
            <div class="form-hint" style="margin-top: 4px; font-size: 11px;">
              {$t('proxies.awg_mtu_hint')}
            </div>
          {/if}
        </div>
      </div>
      <div class="form-row">
        <label class="form-label" for="proxy-wg-preshared-key">Pre-shared Key</label>
        <input
          id="proxy-wg-preshared-key"
          class="form-input"
          bind:value={np.wgPresharedKey}
          placeholder="pre-shared-key (optional)"
        />
      </div>
      <div class="form-row">
        <label
          class="toggle-label"
          style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
        >
          <input
            type="checkbox"
            id="proxy-awg-enabled"
            bind:checked={np.awgEnabled}
            onchange={handleToggleAwg}
          />
          <span>{$t('proxies.awg_section')}</span>
        </label>
      </div>

      {#if np.awgEnabled}
        {@render awgBlock()}
      {/if}
    {:else if np.type === 'socks5' || np.type === 'socks'}
      <div class="form-row">
        <label class="form-label" for="proxy-socks-username"
          >{$t('proxies.username_optional')}</label
        >
        <input
          id="proxy-socks-username"
          class="form-input"
          bind:value={np.username}
          placeholder="username"
        />
      </div>
      <div class="form-row">
        <label class="form-label" for="proxy-socks-password"
          >{$t('proxies.password_optional')}</label
        >
        <input
          id="proxy-socks-password"
          class="form-input"
          bind:value={np.password}
          placeholder="password"
        />
      </div>
    {:else if np.type === 'http'}
      <div class="form-row">
        <label class="form-label" for="proxy-http-username">{$t('proxies.username_optional')}</label
        >
        <input
          id="proxy-http-username"
          class="form-input"
          bind:value={np.username}
          placeholder="username"
        />
      </div>
      <div class="form-row">
        <label class="form-label" for="proxy-http-password">{$t('proxies.password_optional')}</label
        >
        <input
          id="proxy-http-password"
          class="form-input"
          bind:value={np.password}
          placeholder="password"
        />
      </div>
      <div class="form-row">
        <label
          class="toggle-label"
          style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
        >
          <input type="checkbox" bind:checked={np.tls} />
          <span>TLS</span>
        </label>
      </div>
      {#if np.tls}
        <div class="form-row">
          <label
            class="toggle-label"
            style="display: flex; align-items: center; gap: 8px; cursor: pointer; user-select: none;"
          >
            <input type="checkbox" bind:checked={np.skipCertVerify} />
            <span>{$t('editor.skipCertVerify')}</span>
          </label>
        </div>
      {/if}
    {/if}

    <!-- Dialer Proxy Selector (Mihomo dialer-proxy) -->
    <div class="form-row">
      <label class="form-label" for="proxy-dialer">{$t('proxies.dialer_proxy')}</label>
      <select id="proxy-dialer" class="form-select" bind:value={np.dialerProxy}>
        <option value="">{$t('proxies.dialer_none')}</option>
        {#each availableDialers as d}
          <option value={d}>{d}</option>
        {/each}
      </select>
      {#if np.dialerProxy}
        <div
          class="dialer-chain-preview"
          style="margin-top: 6px; font-size: 11px; display: flex; align-items: center; gap: 6px; flex-wrap: wrap;"
        >
          <span style="color: var(--fg-secondary);">{$t('proxies.dialer_chain')}:</span>
          {#each dialerChain.chain as node, idx}
            <span
              class="badge"
              class:badge-primary={idx === 0}
              class:badge-tag={node === 'DIRECT'}
              class:badge-secondary={idx > 0 && node !== 'DIRECT'}
            >
              {node}
            </span>
            {#if idx < dialerChain.chain.length - 1}
              <span style="color: var(--fg-secondary);">→</span>
            {/if}
          {/each}
          {#if dialerChain.hasCycle}
            <span class="badge badge-danger" style="color: var(--danger);">
              {$t('proxies.dialer_cycle_detected')}
            </span>
          {/if}
        </div>
      {/if}
    </div>
  {/if}

  <div class="form-actions">
    <button class="btn btn-secondary" onclick={onCancel}>{$t('app.cancel')}</button>
    <button class="btn btn-primary" onclick={onSave}
      >{isEdit ? $t('app.save') : $t('app.create')}</button
    >
  </div>
</div>

<style>
  .form-card {
    background: transparent;
    border: none;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .imported-identity-box {
    background: var(--bg-surface-hover, rgba(255, 255, 255, 0.03));
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 4px;
  }

  .identity-badge-row {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .mono-endpoint {
    font-family: var(--font-family-mono, monospace);
    font-size: 12px;
    font-weight: 500;
    color: var(--fg-primary);
  }

  .mono-uuid {
    font-family: var(--font-family-mono, monospace);
    font-size: 11px;
    color: var(--fg-secondary);
    background: rgba(255, 255, 255, 0.05);
    padding: 2px 6px;
    border-radius: var(--radius-sm);
  }

  .identity-hint {
    font-size: 11px;
    color: var(--fg-secondary);
    line-height: 1.3;
  }

  .toggle-all-fields-btn {
    margin-left: auto;
    background: none;
    border: none;
    color: var(--primary);
    font-size: 11px;
    cursor: pointer;
    text-decoration: underline;
    padding: 0;
  }

  .toggle-all-fields-btn:hover {
    color: var(--primary-hover, var(--primary));
  }

  .dialer-chain-preview {
    background: rgba(255, 255, 255, 0.02);
    border: 1px dashed var(--border);
    border-radius: var(--radius-sm);
    padding: 6px 10px;
  }

  .form-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .form-row2 {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
  }
  .form-col {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1 1 140px;
    min-width: 140px;
  }
  .form-col-sm {
    flex: 0 1 110px;
    min-width: 100px;
  }

  .form-label {
    font-size: 11px;
    color: var(--fg-dim);
    font-weight: 500;
    text-transform: none;
  }

  .form-input,
  .form-select {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: 13px;
    padding: 6px 10px;
    outline: none;
    width: 100%;
    transition: border-color var(--transition-fast);
  }

  .form-input:focus,
  .form-select:focus {
    border-color: var(--primary);
  }

  .input-with-btn {
    display: flex;
    gap: 6px;
    align-items: center;
  }

  .input-with-btn .form-input {
    flex: 1;
  }

  .btn-gen {
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid var(--border);
    color: var(--fg-secondary);
    border-radius: var(--radius-sm);
    padding: 6px 10px;
    cursor: pointer;
    font-size: 14px;
    transition: background var(--transition-fast);
    flex-shrink: 0;
  }

  .btn-gen:hover {
    background: rgba(255, 255, 255, 0.1);
    color: var(--fg-primary);
  }

  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-size: 13px;
    color: var(--fg-primary);
  }

  .form-actions {
    position: sticky;
    bottom: -20px;
    background: var(--bg-card);
    padding: 12px 0 0 0;
    margin-top: 12px;
    border-top: 1px solid var(--border);
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    z-index: 10;
  }

  .awg-options-block {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    background: var(--bg-surface-raised, rgba(255, 255, 255, 0.03));
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    margin-bottom: 12px;
  }

  .awg-options-block.has-warning {
    border-color: color-mix(in srgb, var(--warning) 50%, var(--border));
  }

  .awg-headers-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 8px;
  }

  @media (max-width: 480px) {
    .awg-headers-grid {
      grid-template-columns: repeat(2, 1fr);
    }
  }

  .awg-group-title {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--fg-secondary);
  }

  .form-hint {
    font-size: 12px;
    color: var(--fg-secondary);
    line-height: 1.4;
    margin: 0;
  }

  .input-warning {
    border-color: var(--warning) !important;
  }

  .awg-subgroup {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px;
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .awg-tokens-grid {
    display: grid;
    grid-template-columns: repeat(5, 1fr);
    gap: 6px;
  }

  @media (max-width: 600px) {
    .awg-tokens-grid {
      grid-template-columns: repeat(3, 1fr);
    }
  }

  .awg-warning-banner {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    font-size: 12px;
    background: color-mix(in srgb, var(--warning) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--warning) 30%, transparent);
    border-radius: var(--radius-sm);
    color: var(--warning);
  }

  .field-error-hint {
    font-size: 11px;
    color: var(--warning);
    margin-top: 2px;
  }

  .awg-preset-bar {
    display: flex;
    align-items: flex-end;
    gap: 12px;
    padding: 10px;
    background: var(--bg-surface-hover, rgba(255, 255, 255, 0.04));
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    margin-bottom: 8px;
  }

  .preset-select-wrap {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .regenerate-btn {
    white-space: nowrap;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 34px;
    flex-shrink: 0;
  }
</style>
