<script lang="ts">
  import { onMount } from 'svelte'
  import { t } from './i18n'
  import { showConfirm } from './stores'
  import PageHeader from './PageHeader.svelte'
  import Icon from './lib/components/Icon.svelte'

  export let onSwitchTab: (tab: string) => void = () => {}

  interface Subscription {
    id: string
    name: string
    url: string
    tag_prefix: string
    interval: number
    last_update: string
    enabled: boolean
    filter_name?: string
    filter_type?: string
    filter_transport?: string
  }

  let subscriptions: Subscription[] = []
  let loading = false
  let refreshLoading: Record<string, boolean> = {}
  let showAddModal = false
  let editingSub: Subscription | null = null

  // Form fields
  let formName = ''
  let formURL = ''
  let formTagPrefix = ''
  let formInterval = 24
  let formFilterName = ''
  let formFilterType = ''
  let formEnabled = true

  async function loadSubscriptions() {
    loading = true
    try {
      const res = await fetch('/api/subscriptions')
      if (res.ok) {
        subscriptions = await res.json()
      }
    } catch (e) {
      // ignore
    } finally {
      loading = false
    }
  }

  async function refreshSubscription(id: string) {
    refreshLoading[id] = true
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      await fetch(`/api/subscriptions/refresh?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      await loadSubscriptions()
    } catch (e) {
      // ignore
    } finally {
      refreshLoading[id] = false
    }
  }

  async function refreshAll() {
    loading = true
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      await fetch('/api/subscriptions/refresh-all', {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      await loadSubscriptions()
    } catch (e) {
      // ignore
    } finally {
      loading = false
    }
  }

  async function saveSubscription() {
    const csrfToken = localStorage.getItem('csrf_token')
    const sub = {
      name: formName,
      url: formURL,
      tag_prefix: formTagPrefix,
      interval: formInterval,
      enabled: formEnabled,
      filter_name: formFilterName || undefined,
      filter_type: formFilterType || undefined,
    }

    try {
      if (editingSub) {
        await fetch(`/api/subscriptions/update?id=${editingSub.id}`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrfToken || ''
          },
          body: JSON.stringify(sub)
        })
      } else {
        await fetch('/api/subscriptions/add', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrfToken || ''
          },
          body: JSON.stringify(sub)
        })
      }
      closeModal()
      await loadSubscriptions()
    } catch (e) {
      // ignore
    }
  }

  async function deleteSubscription(id: string) {
    if (!await showConfirm($t('app.confirm'), $t('subscr.delete_confirm'))) return
    try {
      const csrfToken = localStorage.getItem('csrf_token')
      await fetch(`/api/subscriptions/delete?id=${id}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrfToken || '' }
      })
      await loadSubscriptions()
    } catch (e) {
      // ignore
    }
  }

  function openAddModal() {
    editingSub = null
    formName = ''
    formURL = ''
    formTagPrefix = ''
    formInterval = 24
    formFilterName = ''
    formFilterType = ''
    formEnabled = true
    showAddModal = true
  }

  function openEditModal(sub: Subscription) {
    editingSub = sub
    formName = sub.name
    formURL = sub.url
    formTagPrefix = sub.tag_prefix
    formInterval = sub.interval
    formFilterName = sub.filter_name || ''
    formFilterType = sub.filter_type || ''
    formEnabled = sub.enabled
    showAddModal = true
  }

  function closeModal() {
    showAddModal = false
    editingSub = null
  }

  function formatDate(dateStr: string): string {
    if (!dateStr) return '—'
    const d = new Date(dateStr)
    return d.toLocaleString()
  }

  onMount(() => {
    loadSubscriptions()
  })
</script>

<div class="container">
  <PageHeader
    title={$t('subscr.title')}
    subtitle={$t('subscr.subtitle')}
    breadcrumbs={[{ label: $t('nav.subscriptions') }]}
    {onSwitchTab}
  >
    <div slot="actions" style="display: flex; gap: 0.5rem;">
      <button class="btn btn-secondary" on:click={refreshAll} disabled={loading}>
        {#if loading}{$t('app.loading')}{:else}<Icon name="refresh" size={14} /> {$t('subscr.refresh_all')}{/if}
      </button>
      <button class="btn btn-primary" on:click={openAddModal}>
        + {$t('subscr.add')}
      </button>
    </div>
  </PageHeader>

  {#if subscriptions.length === 0}
    <div class="card text-center" style="padding: 3rem;">
      <p class="text-secondary">{$t('subscr.empty')}</p>
      <button class="btn btn-primary" on:click={openAddModal}>
        + {$t('subscr.add_first')}
      </button>
    </div>
  {:else}
    <div class="subscriptions-list">
      {#each subscriptions as sub}
        <div class="card sub-card">
          <div class="sub-header">
            <div style="display: flex; align-items: center; gap: 0.5rem;">
              <span class="sub-status" class:enabled={sub.enabled}></span>
              <h3 style="margin: 0;">{sub.name}</h3>
            </div>
            <div class="sub-actions">
              <button class="btn-icon" on:click={() => refreshSubscription(sub.id)} disabled={refreshLoading[sub.id]} title={$t('subscr.refresh')}>
                <Icon name="refresh" size={14} />
              </button>
              <button class="btn-icon" on:click={() => openEditModal(sub)} title={$t('app.edit')}>
                <Icon name="edit" size={14} />
              </button>
              <button class="btn-icon" on:click={() => deleteSubscription(sub.id)} title={$t('app.delete')}>
                <Icon name="delete" size={14} />
              </button>
            </div>
          </div>

          <div class="sub-details">
            <div class="sub-detail">
              <span class="sub-label">{$t('subscr.url')}</span>
              <span class="sub-value">{sub.url}</span>
            </div>
            <div class="sub-detail-row">
              <div class="sub-detail">
                <span class="sub-label">{$t('subscr.interval')}</span>
                <span class="sub-value">{sub.interval}h</span>
              </div>
              <div class="sub-detail">
                <span class="sub-label">{$t('subscr.last_update')}</span>
                <span class="sub-value">{formatDate(sub.last_update)}</span>
              </div>
              {#if sub.tag_prefix}
                <div class="sub-detail">
                  <span class="sub-label">{$t('subscr.tag_prefix')}</span>
                  <span class="sub-value">{sub.tag_prefix}</span>
                </div>
              {/if}
            </div>
            {#if sub.filter_name || sub.filter_type}
              <div class="sub-detail">
                <span class="sub-label">{$t('subscr.filters')}</span>
                <span class="sub-value">
                  {#if sub.filter_name}{$t('subscr.filter_name')}: {sub.filter_name}{/if}
                  {#if sub.filter_type} {$t('subscr.filter_type')}: {sub.filter_type}{/if}
                </span>
              </div>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

{#if showAddModal}
  <div class="modal-overlay" role="button" tabindex="0" on:click={closeModal} on:keydown={(e) => e.key === 'Escape' && closeModal()}>
    <div class="modal" role="presentation" on:click|stopPropagation on:keydown|stopPropagation>
      <h3>{editingSub ? $t('subscr.edit_title') : $t('subscr.add_title')}</h3>

      <div class="form-group">
        <label for="form-name" class="form-label">{$t('subscr.name')}</label>
        <input id="form-name" type="text" class="input" bind:value={formName} placeholder={$t('subscr.name_placeholder')} />
      </div>

      <div class="form-group">
        <label for="form-url" class="form-label">{$t('subscr.url')}</label>
        <input id="form-url" type="text" class="input" bind:value={formURL} placeholder="https://..." />
      </div>

      <div class="form-group">
        <label for="form-tag-prefix" class="form-label">{$t('subscr.tag_prefix')}</label>
        <input id="form-tag-prefix" type="text" class="input" bind:value={formTagPrefix} placeholder={$t('subscr.tag_prefix_placeholder')} />
      </div>

      <div class="form-group">
        <label for="form-interval" class="form-label">{$t('subscr.interval')}</label>
        <input id="form-interval" type="number" class="input" bind:value={formInterval} min="1" max="168" />
      </div>

      <div class="form-group">
        <label for="form-filter-name" class="form-label">{$t('subscr.filter_name')}</label>
        <input id="form-filter-name" type="text" class="input" bind:value={formFilterName} placeholder={$t('subscr.filter_placeholder')} />
      </div>

      <div class="form-group">
        <label for="form-filter-type" class="form-label">{$t('subscr.filter_type')}</label>
        <input id="form-filter-type" type="text" class="input" bind:value={formFilterType} placeholder="vmess, vless, trojan..." />
      </div>

      <div class="form-group" style="display: flex; align-items: center; gap: 0.5rem;">
        <input type="checkbox" id="enabled" bind:checked={formEnabled} />
        <label for="enabled">{$t('subscr.enabled')}</label>
      </div>

      <div class="modal-actions">
        <button on:click={closeModal} class="btn btn-secondary">{$t('app.cancel')}</button>
        <button on:click={saveSubscription} class="btn btn-primary">{$t('app.save')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .subscriptions-list {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .sub-card {
    padding: 1rem;
  }

  .sub-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.75rem;
  }

  .sub-status {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--danger);
  }

  .sub-status.enabled {
    background: var(--success);
  }

  .sub-actions {
    display: flex;
    gap: 0.25rem;
  }

  .btn-icon {
    padding: 0.25rem 0.5rem;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 4px;
    cursor: pointer;
    font-size: 0.875rem;
  }

  .sub-details {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .sub-detail-row {
    display: flex;
    gap: 1.5rem;
    flex-wrap: wrap;
  }

  .sub-detail {
    display: flex;
    gap: 0.5rem;
  }

  .sub-label {
    font-size: 0.75rem;
    color: var(--fg-secondary);
    text-transform: uppercase;
  }

  .sub-value {
    font-size: 0.875rem;
    font-family: monospace;
  }

  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0,0,0,0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
  }

  .modal {
    background: var(--card-bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 1.5rem;
    width: 100%;
    max-width: 500px;
    max-height: 90vh;
    overflow-y: auto;
    box-shadow: var(--shadow);
  }

  .modal h3 {
    margin: 0 0 1rem 0;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 1rem;
  }

  .form-group {
    margin-bottom: 0.75rem;
  }

  .form-label {
    display: block;
    font-size: 0.875rem;
    margin-bottom: 0.25rem;
    color: var(--fg-secondary);
  }

  .input {
    width: 100%;
    padding: 0.5rem;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg);
    color: var(--text);
    font-size: 0.875rem;
  }
</style>
