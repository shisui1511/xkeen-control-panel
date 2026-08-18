<script lang="ts">
  import { t } from '../i18n';
  import {
    pingTargetStore,
    setPingTargetConfig,
    isValidUrl,
    type PingPreset
  } from '../lib/pingTargetStore';

  let config = $derived($pingTargetStore);
  let customUrlInput = $state($pingTargetStore.customUrl);
  let customUrlTouched = $state(false);

  let isCustom = $derived(config.preset === 'custom');
  let isCustomValid = $derived(isValidUrl(customUrlInput));
  let showCustomError = $derived(isCustom && customUrlTouched && !isCustomValid);

  function handlePresetChange(preset: PingPreset) {
    setPingTargetConfig({ preset });
  }

  function handleCustomUrlInput(e: Event) {
    customUrlTouched = true;
    const val = (e.target as HTMLInputElement).value;
    customUrlInput = val;
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

<div class="card mb-2 ping-target-card">
  <div class="card-header">
    <div>
      <h3 class="card-title">{$t('settings.ping_target_title')}</h3>
      <p class="card-desc">{$t('settings.ping_target_desc')}</p>
    </div>
  </div>

  <div class="card-body">
    <div class="form-group mb-3">
      <div class="presets-grid">
        <label class="preset-card {config.preset === 'google' ? 'active' : ''}">
          <input
            type="radio"
            name="ping_preset"
            value="google"
            checked={config.preset === 'google'}
            onchange={() => handlePresetChange('google')}
          />
          <div class="preset-info">
            <span class="preset-name">{$t('settings.ping_preset_google')}</span>
            <span class="preset-url">gstatic.com/generate_204</span>
          </div>
        </label>

        <label class="preset-card {config.preset === 'cloudflare' ? 'active' : ''}">
          <input
            type="radio"
            name="ping_preset"
            value="cloudflare"
            checked={config.preset === 'cloudflare'}
            onchange={() => handlePresetChange('cloudflare')}
          />
          <div class="preset-info">
            <span class="preset-name">{$t('settings.ping_preset_cloudflare')}</span>
            <span class="preset-url">cp.cloudflare.com/generate_204</span>
          </div>
        </label>

        <label class="preset-card {config.preset === 'yandex' ? 'active' : ''}">
          <input
            type="radio"
            name="ping_preset"
            value="yandex"
            checked={config.preset === 'yandex'}
            onchange={() => handlePresetChange('yandex')}
          />
          <div class="preset-info">
            <span class="preset-name">{$t('settings.ping_preset_yandex')}</span>
            <span class="preset-url">yandex.ru/generate_204</span>
          </div>
        </label>

        <label class="preset-card {config.preset === 'custom' ? 'active' : ''}">
          <input
            type="radio"
            name="ping_preset"
            value="custom"
            checked={config.preset === 'custom'}
            onchange={() => handlePresetChange('custom')}
          />
          <div class="preset-info">
            <span class="preset-name">{$t('settings.ping_preset_custom')}</span>
            <span class="preset-url">{$t('settings.ping_custom_url_label')}</span>
          </div>
        </label>
      </div>
    </div>

    {#if isCustom}
      <div class="form-group mb-3 custom-url-wrapper">
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
          onblur={() => (customUrlTouched = true)}
        />
        {#if showCustomError}
          <div class="form-error">
            {$t('settings.ping_custom_url_invalid')}
          </div>
        {/if}
      </div>
    {/if}

    <div class="form-group">
      <label for="ping_timeout" class="form-label">
        {$t('settings.ping_timeout_label')}
      </label>
      <select
        id="ping_timeout"
        class="input select"
        value={config.timeoutMs}
        onchange={handleTimeoutChange}
      >
        <option value={2000}>{$t('settings.ping_timeout_2s')}</option>
        <option value={5000}>{$t('settings.ping_timeout_5s')}</option>
        <option value={8000}>{$t('settings.ping_timeout_8s')}</option>
        <option value={10000}>{$t('settings.ping_timeout_10s')}</option>
      </select>
    </div>
  </div>
</div>

<style>
  .card-desc {
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    margin-top: 4px;
  }

  .presets-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 10px;
  }

  .preset-card {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 14px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .preset-card:hover {
    border-color: var(--border-strong);
    background: var(--bg-elevated);
  }

  .preset-card.active {
    border-color: var(--accent);
    background: var(--hover);
  }

  .preset-card input[type='radio'] {
    accent-color: var(--accent);
    width: 16px;
    height: 16px;
    cursor: pointer;
  }

  .preset-info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .preset-name {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .preset-url {
    font-size: 11px;
    color: var(--fg-dim);
    font-family: var(--font-family-mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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
