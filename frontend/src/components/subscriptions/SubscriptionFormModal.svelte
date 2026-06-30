<script lang="ts">
  import { t, currentLang } from '../../i18n';
  import { capabilities } from '../../stores';

  interface Subscription {
    id: string;
    name: string;
    profile_update_hours?: number;
  }

  let {
    editingSub = null,
    formName = $bindable(''),
    formEnableXray = $bindable(false),
    formEnableMihomo = $bindable(false),
    formURL = $bindable(''),
    formInterval = $bindable(24),
    formRoutingMode = $bindable('manual'),
    formTagPrefix = $bindable(''),
    formFilterName = $bindable(''),
    formFilterType = $bindable(''),
    formFilterTransport = $bindable(''),
    formMihomoGroups = $bindable([]),
    formEnabled = $bindable(true),
    formUseProviderInterval = $bindable(false),
    availableMihomoGroups = [],
    onClose,
    onSave
  }: {
    editingSub: Subscription | null;
    formName: string;
    formEnableXray: boolean;
    formEnableMihomo: boolean;
    formURL: string;
    formInterval: number;
    formRoutingMode: 'manual' | 'auto';
    formTagPrefix: string;
    formFilterName: string;
    formFilterType: string;
    formFilterTransport: string;
    formMihomoGroups: string[];
    formEnabled: boolean;
    formUseProviderInterval: boolean;
    availableMihomoGroups: string[];
    onClose: () => void;
    onSave: () => void;
  } = $props();

  let showAdvanced = $state(false);

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      onClose();
    }
  }
</script>

<div
  class="modal-overlay"
  role="button"
  tabindex="0"
  onclick={onClose}
  onkeydown={handleKeydown}
>
  <div class="modal-card" role="presentation" onclick={(e) => e.stopPropagation()}>
    <div class="modal-card-header">
      <h2>{editingSub ? $t('subscr.edit_title') : $t('subscr.add_title')}</h2>
      <button class="modal-close-btn" onclick={onClose}>&times;</button>
    </div>
    <div class="modal-card-body">
      <div class="form-group">
        <label for="form-name" class="form-label">{$t('subscr.name')}</label>
        <input
          id="form-name"
          type="text"
          class="input"
          bind:value={formName}
          placeholder={$t('subscr.name_placeholder')}
        />
      </div>

      <div class="form-group">
        <label class="form-label">{$currentLang === 'ru' ? 'Интеграция в ядра' : 'Kernel Integration'}</label>
        <div style="display: flex; gap: 20px; margin-bottom: 12px;">
          <label style="display: flex; align-items: center; gap: 8px; cursor: pointer; font-size: 13.5px; color: var(--fg-primary);">
            <input type="checkbox" bind:checked={formEnableXray} />
            <span>XRay (JSON / Base64)</span>
          </label>
          <label style="display: flex; align-items: center; gap: 8px; cursor: pointer; font-size: 13.5px; color: var(--fg-primary);">
            <input type="checkbox" bind:checked={formEnableMihomo} />
            <span>Mihomo (Clash YAML)</span>
          </label>
        </div>
        {#if !formEnableXray && !formEnableMihomo}
          <div class="alert alert-danger" style="margin-top: 8px; margin-bottom: 12px; font-size: 12.5px; border-radius: var(--radius-sm); border: 1px solid var(--danger); background: rgba(220, 38, 38, 0.1); color: var(--danger);">
            <strong>{$currentLang === 'ru' ? 'Внимание:' : 'Attention:'}</strong>
            <span>{$t('subscr.no_kernel_warning')}</span>
          </div>
        {/if}
      </div>

      <div class="form-group">
        <label for="form-url" class="form-label">{$t('subscr.url')}</label>
        <input
          id="form-url"
          type="text"
          class="input"
          bind:value={formURL}
          placeholder="https://..."
        />
      </div>

      <div class="form-group">
        <label for="form-interval" class="form-label"
          >{$t('subscr.interval')} ({$currentLang === 'ru' ? 'часов' : 'hours'})</label
        >
        <input
          id="form-interval"
          type="number"
          class="input"
          bind:value={formInterval}
          min="1"
          max="168"
        />
      </div>

      {#if formEnableXray}
        <div class="form-group">
          <label class="form-label">{$currentLang === 'ru' ? 'Режим маршрутизации XRay' : 'XRay Routing Mode'}</label>
          <div class="seg-btn" style="margin-bottom: 12px;">
            <button
              type="button"
              class="seg-opt"
              class:seg-active={formRoutingMode === 'manual'}
              onclick={() => (formRoutingMode = 'manual')}
            >
              {$currentLang === 'ru' ? 'Ручной' : 'Manual'}
            </button>
            <button
              type="button"
              class="seg-opt"
              class:seg-active={formRoutingMode === 'auto'}
              onclick={() => (formRoutingMode = 'auto')}
            >
              {$currentLang === 'ru' ? 'Автоматический (!CN)' : 'Automatic (!CN)'}
            </button>
          </div>
        </div>

        <button
          type="button"
          class="advanced-toggle-btn"
          onclick={() => (showAdvanced = !showAdvanced)}
        >
          <span class="arrow">{showAdvanced ? '▼' : '►'}</span>
          <span>{$t('subscr.advanced_params') || 'Дополнительные параметры'}</span>
        </button>

        {#if showAdvanced}
          <div class="advanced-fields-box">
            <div class="form-group">
              <label for="form-tag-prefix" class="form-label">{$t('subscr.tag_prefix')}</label>
              <input
                id="form-tag-prefix"
                type="text"
                class="input"
                bind:value={formTagPrefix}
                placeholder={$t('subscr.tag_prefix_placeholder')}
              />
            </div>

            <div class="form-group">
              <label for="form-filter-name" class="form-label">{$t('subscr.filter_name')}</label>
              <input
                id="form-filter-name"
                type="text"
                class="input"
                bind:value={formFilterName}
                placeholder={$t('subscr.filter_placeholder')}
              />
            </div>

            <div class="form-group">
              <label for="form-filter-type" class="form-label">{$t('subscr.filter_type')}</label>
              <input
                id="form-filter-type"
                type="text"
                class="input"
                bind:value={formFilterType}
                placeholder="vmess, vless, trojan..."
              />
            </div>

            <div class="form-group">
              <label for="form-filter-transport" class="form-label"
                >{$t('subscr.filter_transport')}</label
              >
              <input
                id="form-filter-transport"
                type="text"
                class="input"
                bind:value={formFilterTransport}
                placeholder="ws, grpc, tcp..."
              />
            </div>
          </div>
        {/if}
      {/if}

      {#if formEnableMihomo}
        <div class="form-group">
          <label class="form-label">{$currentLang === 'ru' ? 'Интегрировать в группы Mihomo' : 'Integrate into Mihomo groups'}</label>
          
          {#if $capabilities?.active_kernel === 'xray'}
            <div class="alert alert-warning" style="margin-bottom: 12px; font-size: 12.5px; border-radius: var(--radius-sm);">
              <strong>{$currentLang === 'ru' ? 'Внимание:' : 'Attention:'}</strong>
              <span>
                {$currentLang === 'ru' 
                  ? ' сейчас запущено ядро Xray, настройки интеграции вступят в силу при переключении на Mihomo' 
                  : ' Xray core is currently running, integration settings will take effect when switching to Mihomo'}
              </span>
            </div>
          {/if}

          {#if availableMihomoGroups.length === 0}
            <div class="alert alert-warning" style="margin-bottom: 12px; font-size: 12.5px; border-radius: var(--radius-sm);">
              <span>
                {$currentLang === 'ru' 
                  ? 'Не удалось найти группы в config.yaml Mihomo. Перейдите в ' 
                  : 'Could not find any groups in Mihomo config.yaml. Please go to the '}
                <a href="#/constructor" onclick={(e) => { e.preventDefault(); onClose(); window.location.hash = '#/constructor'; }} style="text-decoration: underline; color: var(--accent);">
                  {$currentLang === 'ru' ? 'визуальный конструктор' : 'visual constructor'}
                </a>
                {$currentLang === 'ru' ? ' или ' : ' or '}
                <a href="#/editor" onclick={(e) => { e.preventDefault(); onClose(); window.location.hash = '#/editor'; }} style="text-decoration: underline; color: var(--accent);">
                  {$currentLang === 'ru' ? 'текстовый редактор' : 'text editor'}
                </a>
                {$currentLang === 'ru' ? ' для создания групп.' : ' to create groups.'}
              </span>
            </div>
          {/if}

          {#if availableMihomoGroups.length > 0}
            <div class="mihomo-groups-checkboxes" style="display:flex; flex-direction:column; gap:8px; max-height:150px; overflow-y:auto; padding:10px; border:1px solid var(--border); border-radius:var(--radius-sm); background: rgba(0,0,0,0.15);">
              {#each availableMihomoGroups as group}
                <label style="display:flex; align-items:center; gap:8px; cursor:pointer; font-size:13px; color:var(--fg-primary);">
                  <input type="checkbox" checked={formMihomoGroups.includes(group)} onchange={(e) => {
                    if (e.currentTarget.checked) {
                      formMihomoGroups = [...formMihomoGroups, group];
                    } else {
                      formMihomoGroups = formMihomoGroups.filter(g => g !== group);
                    }
                  }} />
                  <span>{group}</span>
                </label>
              {/each}
            </div>
          {/if}
        </div>
      {/if}

      <div class="form-group-checkbox">
        <label class="toggle-switch">
          <input type="checkbox" id="enabled" bind:checked={formEnabled} />
          <span class="toggle-slider"></span>
        </label>
        <label for="enabled" class="checkbox-label">{$t('subscr.enabled')}</label>
      </div>

      <div class="form-group-checkbox">
        <label class="toggle-switch">
          <input
            type="checkbox"
            id="use-provider-interval"
            bind:checked={formUseProviderInterval}
          />
          <span class="toggle-slider"></span>
        </label>
        <label for="use-provider-interval" class="checkbox-label">
          {$t('subscr.use_provider_interval')}
          {#if editingSub && editingSub.profile_update_hours && editingSub.profile_update_hours > 0}
            <span style="color: var(--accent); font-size: 11px; margin-left: 4px;">
              ({$t('subscr.provider_dictates').replace(
                '{hours}',
                String(editingSub.profile_update_hours)
              )})
            </span>
          {/if}
        </label>
      </div>
    </div>
    <div class="modal-card-footer">
      <button class="btn btn-secondary" onclick={onClose}>{$t('app.cancel')}</button>
      <button class="btn btn-primary" onclick={onSave}>{$t('app.save')}</button>
    </div>
  </div>
</div>

<style>
  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
    padding: 20px;
  }

  .modal-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    width: 100%;
    max-width: 520px;
    box-shadow: 0 20px 40px rgba(0, 0, 0, 0.5);
    overflow: hidden;
    display: flex;
    flex-direction: column;
    max-height: 90vh;
    animation: modal-anim 0.2s cubic-bezier(0.16, 1, 0.3, 1);
  }

  @keyframes modal-anim {
    from {
      transform: scale(0.95) translateY(10px);
      opacity: 0;
    }
    to {
      transform: scale(1) translateY(0);
      opacity: 1;
    }
  }

  .modal-card-header {
    padding: 16px 24px;
    border-bottom: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .modal-card-header h2 {
    margin: 0;
    font-size: 16px;
    font-weight: 700;
    color: var(--fg-primary);
  }

  .modal-close-btn {
    background: none;
    border: none;
    color: var(--fg-dim);
    font-size: 24px;
    cursor: pointer;
    line-height: 1;
    padding: 4px;
  }

  .modal-close-btn:hover {
    color: var(--fg-primary);
  }

  .modal-card-body {
    padding: 24px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) var(--bg-card);
  }
  .modal-card-body::-webkit-scrollbar {
    width: 6px;
  }
  .modal-card-body::-webkit-scrollbar-track {
    background: var(--bg-card);
  }
  .modal-card-body::-webkit-scrollbar-thumb {
    background: var(--border-strong);
    border-radius: 4px;
  }
  .modal-card-body::-webkit-scrollbar-thumb:hover {
    background: var(--accent);
  }

  .form-group-checkbox {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 4px;
  }

  .checkbox-label {
    font-size: 13px;
    color: var(--fg-primary);
    cursor: pointer;
    user-select: none;
  }

  .modal-card-footer {
    padding: 16px 24px;
    border-top: 1px solid var(--border);
    display: flex;
    justify-content: flex-end;
    gap: 12px;
  }

  /* Advanced toggle button and fields box */
  .advanced-toggle-btn {
    background: transparent;
    border: none;
    color: var(--accent);
    cursor: pointer;
    font-size: 13px;
    font-weight: 600;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 6px 0;
    margin: 12px 0 6px 0;
    width: 100%;
    text-align: left;
    outline: none;
    transition: color var(--transition-fast);
  }
  .advanced-toggle-btn:hover {
    color: var(--accent-hover, #64b5f6);
  }
  .advanced-toggle-btn .arrow {
    display: inline-block;
    transition: transform var(--transition-fast);
    font-size: 11px;
    width: 12px;
  }
  .advanced-fields-box {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 16px;
    background: rgba(0, 0, 0, 0.15);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    margin-top: 4px;
    margin-bottom: 12px;
  }

  .textarea-link {
    min-height: 90px;
  }
  .preview-section {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .preview-title {
    margin: 0 0 4px 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-secondary);
  }
  .preview-table {
    display: flex;
    flex-direction: column;
    gap: 8px;
    background: rgba(0, 0, 0, 0.15);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 12px 16px;
  }
  .preview-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 13px;
  }
  .preview-label {
    color: var(--fg-secondary);
  }
  .preview-value {
    color: var(--fg-primary);
  }
  .preview-value.code {
    font-family: var(--font-family-mono, monospace);
    font-size: 12px;
  }
  .seg-btn {
    display: flex;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm, 4px);
    overflow: hidden;
    flex-shrink: 0;
  }

  .seg-opt {
    padding: 6px 12px;
    font-size: 13px;
    background: transparent;
    border: none;
    border-right: 1px solid var(--border);
    color: var(--fg-secondary);
    cursor: pointer;
    transition:
      background var(--transition-fast, 0.15s),
      color var(--transition-fast, 0.15s);
    flex: 1;
    text-align: center;
    font-weight: 500;
  }

  .seg-opt:last-child {
    border-right: none;
  }

  .seg-opt:hover {
    background: rgba(255, 255, 255, 0.04);
  }

  .seg-opt.seg-active {
    background: var(--accent);
    color: #fff;
  }
</style>
