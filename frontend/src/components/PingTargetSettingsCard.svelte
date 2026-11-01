<script lang="ts">
  import { t } from '../i18n';
  import Select from './Select.svelte';
  import Section from './Section.svelte';
  import {
    pingTargetStore,
    setPingTargetConfig,
    isValidUrl,
    type PingPreset
  } from '../lib/pingTargetStore';

  const presets: { value: PingPreset; labelKey: string; url?: string }[] = [
    { value: 'google', labelKey: 'settings.ping_preset_google', url: 'gstatic.com/generate_204' },
    {
      value: 'cloudflare',
      labelKey: 'settings.ping_preset_cloudflare',
      url: 'cp.cloudflare.com/generate_204'
    },
    { value: 'yandex', labelKey: 'settings.ping_preset_yandex', url: 'yandex.ru/generate_204' },
    { value: 'custom', labelKey: 'settings.ping_preset_custom' }
  ];

  let config = $derived($pingTargetStore);
  // Поле повторяет customUrl из стора, пока его не трогали; после ввода или ухода
  // фокуса значение фиксируется и стор больше не подставляется.
  let customUrlPinned = $state<string | null>(null);
  let customUrlInput = $derived(customUrlPinned ?? $pingTargetStore.customUrl);
  let customUrlTouched = $derived(customUrlPinned !== null);

  let isCustom = $derived(config.preset === 'custom');
  let isCustomValid = $derived(isValidUrl(customUrlInput));
  let showCustomError = $derived(isCustom && customUrlTouched && !isCustomValid);

  function handlePresetChange(preset: PingPreset) {
    setPingTargetConfig({ preset });
  }

  function handleCustomUrlInput(e: Event) {
    const val = (e.target as HTMLInputElement).value;
    customUrlPinned = val;
    if (isValidUrl(val)) {
      setPingTargetConfig({ customUrl: val.trim() });
    }
  }

  function handleTimeoutChange(e: Event) {
    const val = parseInt((e.target as HTMLSelectElement).value, 10);
    if (!isNaN(val)) {
      setPingTargetConfig({ timeoutMs: val });
    }
  }
</script>

<Section title={$t('settings.ping_target_title')} description={$t('settings.ping_target_desc')}>
  <div class="card card-rows ping-target-card">
    <div class="field-group" role="radiogroup" aria-label={$t('settings.ping_target_title')}>
      {#each presets as p (p.value)}
        <label class="list-row preset-row" class:active={config.preset === p.value}>
          <input
            type="radio"
            name="ping_preset"
            value={p.value}
            checked={config.preset === p.value}
            onchange={() => handlePresetChange(p.value)}
          />
          <span class="preset-name">{$t(p.labelKey)}</span>
          <span class="preset-url">{p.url ?? $t('settings.ping_custom_url_label')}</span>
        </label>
      {/each}
    </div>

    {#if isCustom}
      <div class="card-block custom-url-wrapper">
        <label for="ping_custom_url" class="form-label">
          {$t('settings.ping_custom_url_label')}
        </label>
        <input
          id="ping_custom_url"
          type="url"
          class="input {showCustomError ? 'is-invalid' : ''}"
          placeholder={$t('settings.ping_custom_url_placeholder')}
          value={customUrlInput}
          oninput={handleCustomUrlInput}
          onblur={() => (customUrlPinned ??= customUrlInput)}
        />
        {#if showCustomError}
          <div class="form-error">
            {$t('settings.ping_custom_url_invalid')}
          </div>
        {/if}
      </div>
    {/if}

    <div class="field-group">
      <div class="field-row">
        <label for="ping_timeout" class="field-row-name">
          {$t('settings.ping_timeout_label')}
        </label>
        <Select
          id="ping_timeout"
          class="input select"
          wrapperClass="timeout-select"
          value={String(config.timeoutMs)}
          onchange={handleTimeoutChange}
        >
          <option value="2000">{$t('settings.ping_timeout_2s')}</option>
          <option value="5000">{$t('settings.ping_timeout_5s')}</option>
          <option value="8000">{$t('settings.ping_timeout_8s')}</option>
          <option value="10000">{$t('settings.ping_timeout_10s')}</option>
        </Select>
      </div>
    </div>
  </div>
</Section>

<style>
  /* Пресеты — строки одного списка с радио, а не плитки внутри карточки. */
  .preset-row {
    display: flex;
    align-items: center;
    gap: 12px;
    cursor: pointer;
    transition: background var(--transition-fast);
  }

  .preset-row:hover {
    background: var(--hover);
  }

  .preset-row.active {
    box-shadow: inset 3px 0 0 var(--accent);
  }

  .preset-row input[type='radio'] {
    accent-color: var(--accent);
    width: 16px;
    height: 16px;
    margin: 0;
    flex-shrink: 0;
    cursor: pointer;
  }

  .preset-name {
    font-size: var(--font-size-base);
    font-weight: 500;
    color: var(--fg-primary);
  }

  .preset-url {
    margin-left: auto;
    min-width: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    font-family: var(--font-family-mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .field-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
  }

  .field-row-name {
    font-size: var(--font-size-base);
    font-weight: 500;
    color: var(--fg-primary);
  }

  .field-row :global(.timeout-select) {
    width: auto;
    min-width: 220px;
  }

  .custom-url-wrapper {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .custom-url-wrapper {
    animation: fadeIn 0.15s ease-out;
  }

  @keyframes fadeIn {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  .form-error {
    font-size: var(--font-size-xs);
    color: var(--danger);
    margin-top: 4px;
  }

  .is-invalid {
    border-color: var(--danger) !important;
  }
</style>
