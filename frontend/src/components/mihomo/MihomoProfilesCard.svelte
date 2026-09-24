<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../i18n';
  import { apiFetch, apiFetchJSON } from '../../lib/api';
  import { activateRestartGrace } from '../../lib/serviceGrace';
  import { showConfirm, showToast, editorOpenRequest } from '../../stores';
  import Button from '../Button.svelte';
  import Skeleton from '../Skeleton.svelte';

  interface Profile {
    name: string;
    size: number;
    mtime: number;
    active: boolean;
  }

  interface ProfilesState {
    managed: boolean;
    active?: string;
    profiles: Profile[];
  }

  interface ActivationResult {
    active: string;
    restarted: boolean;
    rolled_back: boolean;
    error?: string;
  }

  interface Props {
    mihomoDir?: string;
    onSwitchTab?: (tab: string) => void;
    /** Called after the core was restarted with another profile. */
    onactivated?: () => void;
  }

  let { mihomoDir = '/opt/etc/mihomo', onSwitchTab = () => {}, onactivated }: Props = $props();

  const NAME_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$/;

  let profilesState = $state<ProfilesState | null>(null);
  let loading = $state(true);
  let loadError = $state('');
  let busy = $state<string | null>(null);

  let newName = $state('');
  let newFromEmpty = $state(false);
  let renaming = $state<string | null>(null);
  let renameTo = $state('');

  const newNameValid = $derived(NAME_RE.test(newName.trim()));

  async function load() {
    loading = true;
    loadError = '';
    try {
      profilesState = await apiFetchJSON<ProfilesState>('/api/mihomo/profiles');
    } catch (e: any) {
      if (e?.status === 401) return;
      loadError = e?.message || String(e);
    } finally {
      loading = false;
    }
  }

  async function post(action: string, body: object): Promise<any> {
    const res = await apiFetch(`/api/mihomo/profiles/${action}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
    const payload = await res.json().catch(() => null);
    if (!res.ok || !payload?.success) {
      throw new Error(payload?.error || `HTTP ${res.status}`);
    }
    return payload.data;
  }

  async function run(key: string, fn: () => Promise<void>) {
    if (busy) return;
    busy = key;
    try {
      await fn();
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', `${$t('profiles.error')}: ${e?.message || e}`, 8000);
    } finally {
      busy = null;
    }
  }

  function create() {
    const name = newName.trim();
    if (!NAME_RE.test(name)) return;
    run('create', async () => {
      profilesState = await post('create', { name, empty: newFromEmpty });
      newName = '';
      showToast('success', $t('profiles.created', { name }));
    });
  }

  async function activate(name: string) {
    const ok = await showConfirm({
      title: $t('profiles.activate_confirm_title', { name }),
      message: $t('profiles.activate_confirm_msg'),
      confirmLabel: $t('profiles.activate'),
      cancelLabel: $t('app.cancel'),
      variant: 'warning'
    });
    if (!ok) return;
    run(`activate:${name}`, async () => {
      activateRestartGrace(30000);
      const res: ActivationResult = await post('activate', { name });
      if (res.error && res.rolled_back) {
        showToast('error', $t('profiles.rolled_back', { error: res.error }), 10000);
      } else if (res.error) {
        showToast('error', $t('profiles.invalid', { error: res.error }), 10000);
      } else {
        showToast('success', $t('profiles.activated', { name: res.active }));
      }
      if (res.restarted) onactivated?.();
      await load();
    });
  }

  function startRename(name: string) {
    renaming = name;
    renameTo = name;
  }

  function confirmRename() {
    const from = renaming;
    const to = renameTo.trim();
    if (!from || !NAME_RE.test(to) || to === from) {
      renaming = null;
      return;
    }
    run(`rename:${from}`, async () => {
      profilesState = await post('rename', { name: from, new_name: to });
      renaming = null;
    });
  }

  async function remove(name: string) {
    const ok = await showConfirm({
      title: $t('profiles.delete_confirm_title', { name }),
      message: $t('profiles.delete_confirm_msg'),
      confirmLabel: $t('profiles.delete'),
      cancelLabel: $t('app.cancel'),
      variant: 'danger'
    });
    if (!ok) return;
    run(`delete:${name}`, async () => {
      profilesState = await post('delete', { name });
    });
  }

  function adopt() {
    run('adopt', async () => {
      profilesState = await post('adopt', { name: 'default' });
      showToast('success', $t('profiles.adopted'));
    });
  }

  function edit(name: string) {
    editorOpenRequest.set(`${mihomoDir}/profiles/${name}.yaml`);
    onSwitchTab('editor');
  }

  function formatDate(unix: number): string {
    return new Date(unix * 1000).toLocaleString();
  }

  onMount(load);
</script>

<div class="card profiles-card">
  <div class="pc-head">
    <h2 class="pc-title">{$t('profiles.title')}</h2>
    <p class="pc-subtitle">{$t('profiles.subtitle')}</p>
  </div>

  {#if loading && !profilesState}
    <Skeleton type="text-line" width="60%" />
  {:else if loadError}
    <div class="pc-error" role="alert">
      <span>{$t('profiles.load_error')}: {loadError}</span>
      <Button variant="secondary" class="btn-sm" onclick={load}>{$t('xkeen_settings.retry')}</Button
      >
    </div>
  {:else if profilesState && !profilesState.managed}
    <div class="pc-unmanaged">
      <p>{$t('profiles.unmanaged')}</p>
      <Button variant="primary" loading={busy === 'adopt'} disabled={!!busy} onclick={adopt}>
        {$t('profiles.adopt')}
      </Button>
    </div>
  {:else if profilesState}
    <ul class="pc-list">
      {#each profilesState.profiles as p (p.name)}
        <li class="pc-row" class:is-active={p.active}>
          <div class="pc-info">
            {#if renaming === p.name}
              <label class="visually-hidden" for="profile-rename-{p.name}">
                {$t('profiles.rename')}
              </label>
              <input
                id="profile-rename-{p.name}"
                class="input font-mono pc-rename"
                bind:value={renameTo}
                onkeydown={(e) => {
                  if (e.key === 'Enter') confirmRename();
                  if (e.key === 'Escape') renaming = null;
                }}
              />
            {:else}
              <span class="pc-name font-mono">{p.name}</span>
            {/if}
            {#if p.active}
              <span class="badge pc-badge">{$t('profiles.active')}</span>
            {/if}
            <span class="pc-meta">{formatDate(p.mtime)} · {(p.size / 1024).toFixed(1)} KB</span>
          </div>
          <div class="pc-actions">
            {#if renaming === p.name}
              <Button variant="primary" class="btn-sm" onclick={confirmRename}>
                {$t('profiles.save')}
              </Button>
              <Button variant="secondary" class="btn-sm" onclick={() => (renaming = null)}>
                {$t('app.cancel')}
              </Button>
            {:else}
              {#if !p.active}
                <Button
                  variant="primary"
                  class="btn-sm"
                  loading={busy === `activate:${p.name}`}
                  disabled={!!busy}
                  onclick={() => activate(p.name)}
                >
                  {$t('profiles.activate')}
                </Button>
              {/if}
              <Button variant="secondary" class="btn-sm" onclick={() => edit(p.name)}>
                {$t('profiles.edit')}
              </Button>
              <Button
                variant="secondary"
                class="btn-sm"
                disabled={!!busy}
                onclick={() => startRename(p.name)}
              >
                {$t('profiles.rename')}
              </Button>
              {#if !p.active}
                <Button
                  variant="danger"
                  class="btn-sm"
                  loading={busy === `delete:${p.name}`}
                  disabled={!!busy}
                  onclick={() => remove(p.name)}
                >
                  {$t('profiles.delete')}
                </Button>
              {/if}
            {/if}
          </div>
        </li>
      {/each}
    </ul>

    <form
      class="pc-create"
      onsubmit={(e) => {
        e.preventDefault();
        create();
      }}
    >
      <label class="visually-hidden" for="profile-new-name">{$t('profiles.new_name')}</label>
      <input
        id="profile-new-name"
        class="input font-mono"
        placeholder={$t('profiles.new_name')}
        bind:value={newName}
        autocomplete="off"
        spellcheck="false"
      />
      <label class="pc-check">
        <input type="checkbox" bind:checked={newFromEmpty} />
        <span>{$t('profiles.from_empty')}</span>
      </label>
      <Button
        type="submit"
        variant="secondary"
        loading={busy === 'create'}
        disabled={!!busy || !newNameValid}
      >
        {newFromEmpty ? $t('profiles.create_empty') : $t('profiles.create_copy')}
      </Button>
    </form>
    {#if newName && !newNameValid}
      <p class="pc-hint">{$t('profiles.name_rules')}</p>
    {/if}
  {/if}
</div>

<style>
  .profiles-card {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .pc-title {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: 700;
    color: var(--fg-primary);
  }

  .pc-subtitle,
  .pc-hint,
  .pc-unmanaged p {
    margin: 2px 0 0;
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .pc-error {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-3);
    color: var(--danger);
    font-size: var(--font-size-sm);
  }

  .pc-unmanaged {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-3);
  }

  .pc-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .pc-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
    padding: var(--spacing-2) var(--spacing-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-card-subtle);
  }

  .pc-row.is-active {
    border-color: var(--accent-border);
  }

  .pc-info {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .pc-name {
    font-weight: 600;
    color: var(--fg-primary);
    word-break: break-all;
  }

  .pc-badge {
    background: var(--accent-soft);
    color: var(--accent-text);
  }

  .pc-meta {
    font-size: var(--font-size-xs);
    color: var(--fg-muted);
  }

  .pc-rename {
    width: 14rem;
    max-width: 100%;
  }

  .pc-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-2);
  }

  .pc-create {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
  }

  .pc-create .input {
    flex: 1 1 12rem;
    min-width: 0;
  }

  .pc-check {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-1);
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
  }

  .visually-hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }
</style>
