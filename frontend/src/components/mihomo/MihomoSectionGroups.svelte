<script lang="ts">
  import Modal from '../Modal.svelte';
  import Icon from '../../lib/components/Icon.svelte';
  import GroupForm from './GroupForm.svelte';
  import Select from '../Select.svelte';
  import { t } from '../../i18n';
  import { showToast } from '../../stores';
  import { getMihomoContext } from './MihomoContext.svelte';
  import type { ProxyGroup } from '../../lib/mihomoYaml';

  let ctx = getMihomoContext();

  let showGroupForm = $state(false);
  let editingGroupId = $state<string | null>(null);

  function newGroupDefaults(): Omit<ProxyGroup, 'id'> {
    return {
      name: '',
      type: 'select',
      proxies: [],
      includeAll: false,
      url: 'https://www.gstatic.com/generate_204',
      interval: 300,
      useProviders: [],
      strategy: undefined
    };
  }

  let ng = $state<Omit<ProxyGroup, 'id'>>(newGroupDefaults());

  const allProxyNames = $derived([
    'DIRECT',
    'REJECT',
    ...ctx.proxies.map((p) => p.name),
    ...ctx.groups.map((g) => g.name)
  ]);

  function openAddGroup() {
    editingGroupId = null;
    ng = newGroupDefaults();
    showGroupForm = true;
  }

  function editGroup(g: ProxyGroup) {
    editingGroupId = g.id;
    ng = { ...g };
    showGroupForm = true;
  }

  function saveGroup() {
    if (!ng.name.trim()) return;
    if (editingGroupId) {
      ctx.updateGroup(editingGroupId, { ...ng, name: ng.name.trim() });
    } else {
      ctx.addGroup({
        ...ng,
        id: crypto.randomUUID(),
        name: ng.name.trim()
      });
    }
    showGroupForm = false;
    editingGroupId = null;
  }
</script>

<div class="sec-body" data-testid="mihomo-section-groups">
  {#if ctx.activeRuleProvider === 'zkeen'}
    <!-- Premium zkeen 16 groups UI -->
    <div class="zkeen-groups-grid">
      {#each ctx.groups as g (g.id)}
        <div class="zkeen-group-card" class:disabled={g.enabled === false}>
          <div class="zkeen-group-header">
            <div class="zkeen-group-icon-wrap">
              <img
                src={g.icon}
                alt={g.name}
                class="zkeen-group-icon"
                onerror={() => {
                  const fallback =
                    'https://raw.githubusercontent.com/Koolson/Qure/master/IconSet/Color/Global.png';
                  if (g.icon !== fallback) {
                    g.icon = fallback;
                  } else {
                    g.icon =
                      'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7';
                  }
                  ctx.groups = [...ctx.groups];
                }}
              />
            </div>
            <div class="zkeen-group-title">
              <span class="zkeen-group-name">{g.name}</span>
              <div style="display: flex; gap: 4px; flex-wrap: wrap; align-items: center;">
                {#if (g.type as string) === 'relay'}
                  <span
                    class="item-badge badge-warning"
                    style="text-transform: none;"
                    title={$t('mihomo.warnings.relay_deprecated', { name: g.name })}
                    >relay · {$t('app.deprecated')}</span
                  >
                {/if}
                {#if g.excludeFilter}
                  <span class="zkeen-exclude-badge">exclude: {g.excludeFilter}</span>
                {/if}
                {#if g.includeAll}
                  <span class="zkeen-include-badge">include-all</span>
                {/if}
              </div>
            </div>
            <label class="switch">
              <input
                type="checkbox"
                checked={g.enabled !== false}
                onchange={(e) => {
                  g.enabled = e.currentTarget.checked;
                  if (g.enabled === false) {
                    showToast('warning', $t('editor.group_disable_warning', { group: g.name }));
                  }
                  ctx.markDirty();
                }}
              />
              <span class="slider round"></span>
            </label>
          </div>

          {#if g.enabled !== false}
            <div class="zkeen-group-body">
              <label
                for="mihomo-group-default-outbound-{g.name}"
                class="form-label"
                style="font-size: var(--font-size-xs); margin-bottom: 2px;"
                >{$t('mihomo.default_outbound')}</label
              >
              <Select
                id="mihomo-group-default-outbound-{g.name}"
                class="form-select"
                value={g.proxies[0] || 'DIRECT'}
                onchange={(e) => {
                  const val = e.currentTarget.value;
                  g.proxies = [val, ...g.proxies.slice(1).filter((p) => p !== val)];
                  ctx.markDirty();
                }}
              >
                <option value="DIRECT">DIRECT</option>
                <option value="REJECT">REJECT</option>
                {#each allProxyNames.filter((n) => n !== 'DIRECT' && n !== 'REJECT' && n !== g.name) as n, i (i)}
                  <option value={n}>{n}</option>
                {/each}
              </Select>
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {:else}
    {#each ctx.groups as g (g.id)}
      <div class="item-row">
        <span class="item-badge type-group">{g.type}</span>
        {#if (g.type as string) === 'relay'}
          <span
            class="item-badge badge-warning"
            style="text-transform: none;"
            title={$t('mihomo.warnings.relay_deprecated', { name: g.name })}
            >{$t('app.deprecated')}</span
          >
        {/if}
        <span class="item-name">{g.name}</span>
        {#if g.includeAll}
          <span class="item-badge badge-include-all">include-all</span>
        {/if}
        {#if g.useProviders && g.useProviders.length > 0}
          <span
            class="item-badge"
            style="background: color-mix(in srgb, var(--success) 20%, transparent); color: var(--success); font-size: var(--font-size-xs); text-transform: none;"
            title={g.useProviders.join(', ')}>use: {g.useProviders.length}</span
          >
        {/if}
        {#if g.type === 'load-balance' && g.strategy}
          <span
            class="item-badge"
            style="background: color-mix(in srgb, var(--warning) 20%, transparent); color: var(--warning); font-size: var(--font-size-xs); text-transform: none;"
            >{g.strategy}</span
          >
        {/if}
        <span class="item-meta">{g.proxies.length} {$t('mihomo.proxies_count')}</span>
        <button class="item-edit" onclick={() => editGroup(g)} title={$t('app.edit')}
          ><Icon name="edit" size={13} /></button
        >
        <button class="item-del" onclick={() => ctx.removeGroup(g.id)} title={$t('app.delete')}
          ><Icon name="close" size={12} /></button
        >
      </div>
    {/each}

    <div style="margin-top: 12px;">
      <button type="button" class="add-btn btn-secondary" onclick={openAddGroup}>
        + {$t('mihomo.add_group')}
      </button>
    </div>
  {/if}
</div>

<!-- Modal Group Form -->
<Modal
  open={showGroupForm}
  title={editingGroupId
    ? $t('mihomo.edit_group_title', { name: ng.name || 'group' })
    : $t('mihomo.new_group_title')}
  onclose={() => {
    showGroupForm = false;
    editingGroupId = null;
  }}
>
  <GroupForm
    bind:ng
    {allProxyNames}
    mihomoProviders={ctx.mihomoProviders}
    isEdit={!!editingGroupId}
    onSave={saveGroup}
    onCancel={() => {
      showGroupForm = false;
      editingGroupId = null;
    }}
  />
</Modal>

<style>
  .sec-body {
    padding: var(--spacing-4);
  }

  .zkeen-groups-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: var(--spacing-3);
    margin-bottom: var(--spacing-4);
  }

  .zkeen-group-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: var(--spacing-3);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    transition:
      border-color var(--transition-fast),
      opacity var(--transition-fast);
  }

  .zkeen-group-card.disabled {
    opacity: 0.6;
    border-color: var(--border);
  }

  .zkeen-group-header {
    display: flex;
    align-items: center;
    gap: var(--spacing-2-5);
  }

  .zkeen-group-icon-wrap {
    width: 24px;
    height: 24px;
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: var(--surface-tint);
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .zkeen-group-icon {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }

  .zkeen-group-title {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-0-5);
  }

  .zkeen-group-name {
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .zkeen-exclude-badge {
    background: color-mix(in srgb, var(--warning) 12%, transparent);
    color: var(--warning);
    font-size: var(--font-size-xs);
    padding: 1px var(--spacing-1);
    border-radius: var(--radius-sm);
    width: fit-content;
  }

  .zkeen-include-badge {
    background: color-mix(in srgb, var(--seq-6) 12%, transparent);
    color: var(--seq-6);
    font-size: var(--font-size-xs);
    padding: 1px var(--spacing-1);
    border-radius: var(--radius-sm);
    width: fit-content;
  }

  .zkeen-group-body {
    margin-top: var(--spacing-1);
    border-top: 1px solid var(--border-subtle);
    padding-top: var(--spacing-2);
  }

  .item-row {
    display: flex;
    align-items: center;
    gap: var(--spacing-2-5);
    padding: var(--spacing-2) var(--spacing-3);
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    margin-bottom: var(--spacing-1-5);
  }

  .item-badge {
    font-size: var(--font-size-xs);
    padding: var(--spacing-0-5) var(--spacing-1-5);
    border-radius: var(--radius-sm);
    font-weight: 500;
    flex-shrink: 0;
  }

  .type-group {
    background: color-mix(in srgb, var(--seq-1) 15%, transparent);
    color: var(--seq-1);
  }

  .badge-warning {
    background: color-mix(in srgb, var(--warning) 20%, transparent);
    color: var(--warning);
  }

  .badge-include-all {
    background: color-mix(in srgb, var(--seq-5) 20%, transparent);
    color: var(--seq-5);
    font-size: var(--font-size-xs);
    text-transform: none;
  }

  .item-name {
    flex: 1;
    min-width: 50px;
    font-size: var(--font-size-sm);
    font-weight: 500;
    color: var(--fg-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .item-meta {
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    /* Long values (provider URLs, server addresses) shrink with an ellipsis
       instead of pushing the row actions off a phone screen. */
    min-width: 0;
    max-width: 50%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .item-edit,
  .item-del {
    background: none;
    border: none;
    color: var(--fg-faint);
    cursor: pointer;
    font-size: var(--font-size-xs);
    padding: var(--spacing-0-5) var(--spacing-1);
    border-radius: var(--radius-sm);
    transition: color var(--transition-fast);
    flex-shrink: 0;
    line-height: 1;
    display: inline-flex;
    align-items: center;
  }

  .item-edit:hover {
    color: var(--accent);
  }

  .item-del:hover {
    color: var(--danger);
  }

  .add-btn {
    font-size: var(--font-size-xs);
    padding: var(--spacing-1-5) var(--spacing-3);
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition:
      color var(--transition-fast),
      background-color var(--transition-fast),
      border-color var(--transition-fast),
      box-shadow var(--transition-fast),
      opacity var(--transition-fast),
      transform var(--transition-fast);
  }

  .btn-secondary {
    background: var(--bg-card);
    border: 1px solid var(--border);
    color: var(--fg-primary);
  }

  .btn-secondary:hover {
    background: var(--bg-hover);
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
    background-color: var(--hover);
    transition: var(--transition-normal);
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
    transition: var(--transition-normal);
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
</style>
