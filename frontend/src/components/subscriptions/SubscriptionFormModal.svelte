<script lang="ts">
  import Toggle from '../Toggle.svelte';
  import Modal from '../Modal.svelte';
  import SegmentedControl from '../SegmentedControl.svelte';
  import { t, currentLang } from '../../i18n';
  import { isXray } from '../../stores';
  import { editorHash } from '../../lib/editorRoute';

  interface Subscription {
    id: string;
    name: string;
    profile_update_hours?: number;
  }

  let {
    open: isOpen = false,
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
    formExcludeFilter = $bindable(''),
    formExcludeType = $bindable(''),
    formMihomoGroups = $bindable([]),
    formEnabled = $bindable(true),
    formUseProviderInterval = $bindable(false),
    formSockoptMark = $bindable<number | null>(null),
    formSockoptFastOpen = $bindable(false),
    formSockoptMptcp = $bindable(false),
    availableMihomoGroups = [],
    onclose: onClose,
    onSave
  }: {
    open?: boolean;
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
    formExcludeFilter?: string;
    formExcludeType?: string;
    formMihomoGroups: string[];
    formEnabled: boolean;
    formUseProviderInterval: boolean;
    formSockoptMark?: number | null;
    formSockoptFastOpen?: boolean;
    formSockoptMptcp?: boolean;
    availableMihomoGroups?: string[];
    onclose: () => void;
    onSave: () => void;
  } = $props();

  let showAdvanced = $state(false);
  let missingGroups = $derived(
    (formMihomoGroups || []).filter((g) => !(availableMihomoGroups || []).includes(g))
  );
</script>

<Modal
  open={isOpen}
  title={editingSub ? $t('subscr.edit_title') : $t('subscr.add_title')}
  onclose={onClose}
>
  <div style="display: flex; flex-direction: column; gap: 16px;">
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
      <span class="form-label" style="display: block; margin-bottom: 6px;"
        >{$t('subscr.modal.kernel_integration')}</span
      >
      <div style="display: flex; gap: 20px; margin-bottom: 12px;">
        <label
          style="display: flex; align-items: center; gap: 8px; cursor: pointer; font-size: var(--font-size-base); color: var(--fg-primary);"
        >
          <input type="checkbox" bind:checked={formEnableXray} />
          <span>XRay (JSON / Base64)</span>
        </label>
        <label
          style="display: flex; align-items: center; gap: 8px; cursor: pointer; font-size: var(--font-size-base); color: var(--fg-primary);"
        >
          <input type="checkbox" bind:checked={formEnableMihomo} />
          <span>Mihomo (Clash YAML)</span>
        </label>
      </div>
      {#if !formEnableXray && !formEnableMihomo}
        <div
          class="alert alert-danger"
          style="margin-top: 8px; margin-bottom: 12px; font-size: var(--font-size-sm); border-radius: var(--radius-sm); border: 1px solid var(--danger); background: color-mix(in srgb, var(--danger) 10%, transparent); color: var(--danger);"
        >
          <strong>{$t('subscr.modal.attention')}</strong>
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
        >{$t('subscr.interval')} ({$t('subscr.modal.hours')})</label
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
        <span class="form-label" style="display: block; margin-bottom: 6px;"
          >{$t('subscr.modal.xray_routing_mode')}</span
        >
        <div style="margin-bottom: 12px;">
          <SegmentedControl
            bind:value={formRoutingMode}
            items={[
              { value: 'manual', label: $t('subscr.modal.manual') },
              { value: 'auto', label: $t('subscr.modal.auto_cn') }
            ]}
            ariaLabel={$t('subscr.modal.xray_routing_mode')}
          />
        </div>
      </div>
    {/if}

    {#if formEnableXray || formEnableMihomo}
      <button
        type="button"
        class="advanced-toggle-btn"
        onclick={() => (showAdvanced = !showAdvanced)}
      >
        <span class="arrow">{showAdvanced ? '▼' : '►'}</span>
        <span>{$t('subscr.advanced_params')}</span>
      </button>

      {#if showAdvanced}
        <div class="advanced-fields-box">
          {#if formEnableXray}
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
          {/if}

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
            <label for="form-exclude-filter" class="form-label">{$t('subscr.exclude_filter')}</label
            >
            <input
              id="form-exclude-filter"
              type="text"
              class="input"
              bind:value={formExcludeFilter}
              placeholder={$t('subscr.exclude_placeholder')}
            />
          </div>

          {#if formEnableXray}
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
          {/if}

          <div class="form-group">
            <label for="form-exclude-type" class="form-label">{$t('subscr.exclude_type')}</label>
            <input
              id="form-exclude-type"
              type="text"
              class="input"
              bind:value={formExcludeType}
              placeholder={$t('subscr.exclude_type_placeholder')}
            />
          </div>

          {#if formEnableXray}
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
          {/if}
        </div>
      {/if}
    {/if}

    {#if formEnableMihomo}
      <div class="form-group">
        <span class="form-label" style="display: block; margin-bottom: 6px;"
          >{$t('subscr.modal.integrate_mihomo_groups')}</span
        >

        {#if $isXray}
          <div
            class="alert alert-warning"
            style="margin-bottom: 12px; font-size: var(--font-size-sm); border-radius: var(--radius-sm);"
          >
            <strong>{$t('subscr.modal.attention')}</strong>
            <span>
              {$t('subscr.modal.xray_active_notice')}
            </span>
          </div>
        {/if}

        {#if availableMihomoGroups.length === 0 && missingGroups.length === 0}
          <div
            class="alert alert-warning"
            style="margin-bottom: 12px; font-size: var(--font-size-sm); border-radius: var(--radius-sm);"
          >
            <span>
              {$t('subscr.modal.no_mihomo_groups_prefix')}
              <a
                href="#/constructor"
                onclick={(e) => {
                  e.preventDefault();
                  onClose();
                  window.location.hash = '#/constructor';
                }}
                style="text-decoration: underline; color: var(--accent);"
              >
                {$t('subscr.modal.visual_constructor')}
              </a>
              {$t('app.or')}
              <a
                href={editorHash()}
                onclick={(e) => {
                  e.preventDefault();
                  onClose();
                  window.location.hash = editorHash();
                }}
                style="text-decoration: underline; color: var(--accent);"
              >
                {$t('subscr.modal.text_editor')}
              </a>
              {$t('subscr.modal.no_mihomo_groups_suffix')}
            </span>
          </div>
        {/if}

        {#if availableMihomoGroups.length > 0 || missingGroups.length > 0}
          <div
            class="mihomo-groups-checkboxes"
            style="display:flex; flex-direction:column; gap:8px; max-height:150px; overflow-y:auto; padding:10px; border:1px solid var(--border); border-radius:var(--radius-sm); background: rgba(0,0,0,0.15);"
          >
            {#each availableMihomoGroups as group, i (i)}
              <label
                style="display:flex; align-items:center; gap:8px; cursor:pointer; font-size:var(--font-size-sm); color:var(--fg-primary);"
              >
                <input
                  type="checkbox"
                  checked={formMihomoGroups.includes(group)}
                  onchange={(e) => {
                    if (e.currentTarget.checked) {
                      formMihomoGroups = [...formMihomoGroups, group];
                    } else {
                      formMihomoGroups = formMihomoGroups.filter((g) => g !== group);
                    }
                  }}
                />
                <span>{group}</span>
              </label>
            {/each}
            {#each missingGroups as missingGroup, i (i)}
              <label
                style="display:flex; align-items:center; gap:8px; cursor:pointer; font-size:var(--font-size-sm); color:var(--fg-dim);"
              >
                <input
                  type="checkbox"
                  checked={true}
                  onchange={(e) => {
                    if (!e.currentTarget.checked) {
                      formMihomoGroups = formMihomoGroups.filter((g) => g !== missingGroup);
                    }
                  }}
                />
                <span>{missingGroup}</span>
                <span class="badge badge--tag missing-group-badge">
                  ({$t('subscr.modal.group_missing_in_config')})
                </span>
              </label>
            {/each}
          </div>
        {/if}
      </div>
    {/if}

    {#if formEnableXray}
      <!-- Sockopt Settings (D-12) -->
      <div
        class="form-group sockopt-settings-group"
        data-testid="sockopt-settings-group"
        style="border-top: 1px solid var(--border-subtle); padding-top: 12px;"
      >
        <span class="form-label" style="display: block; margin-bottom: 4px;"
          >{$t('subscr.sockopt.title')}</span
        >
        <div
          class="form-hint"
          style="font-size: var(--font-size-xs); color: var(--fg-dim); margin-bottom: 12px;"
        >
          {$t('subscr.sockopt.hint')}
        </div>

        <div class="form-group" style="margin-bottom: 12px;">
          <label for="sockopt-mark" class="form-label">{$t('subscr.sockopt.mark')}</label>
          <input
            id="sockopt-mark"
            type="number"
            class="input"
            data-testid="sockopt-mark"
            bind:value={formSockoptMark}
            placeholder="0"
            min="0"
            max="2147483647"
          />
          <div
            class="form-hint"
            style="font-size: var(--font-size-xs); color: var(--fg-faint); margin-top: 4px;"
          >
            {$t('subscr.sockopt.mark_hint')}
          </div>
        </div>

        <div class="form-group-checkbox" style="margin-bottom: 8px;">
          <Toggle
            id="sockopt-fast-open"
            aria-label={$t('subscr.sockopt.tcp_fast_open')}
            data-testid="sockopt-fast-open"
            bind:checked={formSockoptFastOpen}
          />
          <label for="sockopt-fast-open" class="checkbox-label">
            {$t('subscr.sockopt.tcp_fast_open')}
          </label>
        </div>

        <div class="form-group-checkbox">
          <Toggle
            id="sockopt-mptcp"
            aria-label={$t('subscr.sockopt.tcp_mptcp')}
            data-testid="sockopt-mptcp"
            bind:checked={formSockoptMptcp}
          />
          <label for="sockopt-mptcp" class="checkbox-label">
            {$t('subscr.sockopt.tcp_mptcp')}
          </label>
        </div>
      </div>
    {/if}

    <div class="form-group-checkbox">
      <Toggle id="enabled" aria-label={$t('subscr.enabled')} bind:checked={formEnabled} />
      <label for="enabled" class="checkbox-label">{$t('subscr.enabled')}</label>
    </div>

    <div class="form-group-checkbox">
      <Toggle
        id="use-provider-interval"
        aria-label={$t('subscr.use_provider_interval')}
        bind:checked={formUseProviderInterval}
      />
      <label for="use-provider-interval" class="checkbox-label">
        {$t('subscr.use_provider_interval')}
        {#if editingSub && editingSub.profile_update_hours && editingSub.profile_update_hours > 0}
          <span style="color: var(--accent); font-size: var(--font-size-xs); margin-left: 4px;">
            ({$t('subscr.provider_dictates').replace(
              '{hours}',
              String(editingSub.profile_update_hours)
            )})
          </span>
        {/if}
      </label>
    </div>
    <div style="display: flex; justify-content: flex-end; gap: 12px; margin-top: 16px;">
      <button class="btn btn-secondary" onclick={onClose}>{$t('app.cancel')}</button>
      <button class="btn btn-primary" onclick={onSave}>{$t('app.save')}</button>
    </div>
  </div>
</Modal>

<style>
  .form-group-checkbox {
    display: flex;
    align-items: center;
    gap: var(--spacing-3);
    margin-top: var(--spacing-1);
  }

  .checkbox-label {
    font-size: var(--font-size-sm);
    color: var(--fg-primary);
    cursor: pointer;
    user-select: none;
  }

  /* Advanced toggle button and fields box */
  .advanced-toggle-btn {
    background: transparent;
    border: none;
    color: var(--accent);
    cursor: pointer;
    font-size: var(--font-size-sm);
    font-weight: 600;
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
    padding: var(--spacing-1-5) 0;
    margin: var(--spacing-3) 0 var(--spacing-1-5) 0;
    width: 100%;
    text-align: left;
    transition: color var(--transition-fast);
  }
  .advanced-toggle-btn:hover {
    color: var(--accent-hover);
  }
  .advanced-toggle-btn .arrow {
    display: inline-block;
    transition: transform var(--transition-fast);
    font-size: var(--font-size-xs);
    width: 12px;
  }
  .advanced-fields-box {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    padding: var(--spacing-4);
    background: rgba(0, 0, 0, 0.15);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    margin-top: var(--spacing-1);
    margin-bottom: var(--spacing-3);
  }

  .missing-group-badge {
    font-size: var(--font-size-xs);
    padding: 1px var(--spacing-1-5);
    background: color-mix(in srgb, var(--danger) 15%, transparent);
    color: var(--danger);
    border: 1px solid color-mix(in srgb, var(--danger) 30%, transparent);
    border-radius: var(--radius-xs);
  }
</style>
