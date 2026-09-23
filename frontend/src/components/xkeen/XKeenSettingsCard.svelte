<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../i18n';
  import { apiFetch, apiFetchJSON } from '../../lib/api';
  import { activateRestartGrace } from '../../lib/serviceGrace';
  import { showToast } from '../../stores';
  import Button from '../Button.svelte';
  import SegmentedControl from '../SegmentedControl.svelte';
  import Skeleton from '../Skeleton.svelte';

  type Kind = 'port_proxying' | 'port_exclude' | 'ip_exclude' | 'xkeen_json';

  interface Issue {
    line?: number;
    severity: 'error' | 'warning';
    code: string;
    value?: string;
  }

  interface SettingsFile {
    kind: Kind;
    path: string;
    exists: boolean;
    content: string;
    entries: number;
    issues: Issue[];
  }

  interface Props {
    /** Called after XKeen was restarted so the parent can refresh its status. */
    onrestarted?: () => void;
  }

  let { onrestarted }: Props = $props();

  const KINDS: Kind[] = ['port_proxying', 'port_exclude', 'ip_exclude', 'xkeen_json'];
  const VALIDATE_DELAY_MS = 400;

  let files = $state<Record<string, SettingsFile>>({});
  let drafts = $state<Record<string, string>>({});
  let liveIssues = $state<Record<string, Issue[]>>({});
  let liveEntries = $state<Record<string, number>>({});
  let activeKind = $state<Kind>('port_proxying');
  let loading = $state(true);
  let loadError = $state('');
  let saving = $state(false);

  let validateTimer: ReturnType<typeof setTimeout> | undefined;
  let validateSeq = 0;

  const current = $derived(files[activeKind]);
  const draft = $derived(drafts[activeKind] ?? '');
  const dirty = $derived(!!current && draft !== current.content);
  const issues = $derived(liveIssues[activeKind] ?? current?.issues ?? []);
  const entries = $derived(liveEntries[activeKind] ?? current?.entries ?? 0);
  const errorCount = $derived(issues.filter((i) => i.severity === 'error').length);

  const segments = $derived(
    KINDS.map((kind) => ({
      value: kind,
      label:
        drafts[kind] !== undefined && files[kind] && drafts[kind] !== files[kind].content
          ? `${$t(`xkeen_settings.kind.${kind}`)} •`
          : $t(`xkeen_settings.kind.${kind}`)
    }))
  );

  async function load() {
    loading = true;
    loadError = '';
    try {
      const list = await apiFetchJSON<SettingsFile[]>('/api/xkeen/settings');
      const next: Record<string, SettingsFile> = {};
      const nextDrafts: Record<string, string> = {};
      for (const f of list) {
        next[f.kind] = f;
        nextDrafts[f.kind] = f.content;
      }
      files = next;
      drafts = nextDrafts;
      liveIssues = {};
      liveEntries = {};
    } catch (e: any) {
      loadError = e?.message || String(e);
    } finally {
      loading = false;
    }
  }

  function scheduleValidate(kind: Kind, content: string) {
    clearTimeout(validateTimer);
    validateTimer = setTimeout(() => validate(kind, content), VALIDATE_DELAY_MS);
  }

  async function validate(kind: Kind, content: string) {
    const seq = ++validateSeq;
    try {
      const res = await apiFetchJSON<{ entries: number; issues: Issue[] }>(
        '/api/xkeen/settings/validate',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ kind, content })
        }
      );
      if (seq !== validateSeq) return;
      liveIssues[kind] = res.issues;
      liveEntries[kind] = res.entries;
    } catch {
      // Validation is advisory; the save request re-validates on the server.
    }
  }

  function handleInput(e: Event) {
    const value = (e.currentTarget as HTMLTextAreaElement).value;
    drafts[activeKind] = value;
    scheduleValidate(activeKind, value);
  }

  function revert() {
    if (!current) return;
    drafts[activeKind] = current.content;
    delete liveIssues[activeKind];
    delete liveEntries[activeKind];
  }

  async function save(restart: boolean) {
    const kind = activeKind;
    saving = true;
    try {
      const res = await apiFetch('/api/xkeen/settings/save', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ kind, content: drafts[kind] ?? '' })
      });
      const payload = await res.json().catch(() => null);
      if (res.status === 422 && payload?.data) {
        liveIssues[kind] = payload.data.issues ?? [];
        liveEntries[kind] = payload.data.entries ?? 0;
        showToast('error', $t('xkeen_settings.save_invalid'));
        return;
      }
      if (!res.ok || !payload?.success) {
        throw new Error(payload?.error || `HTTP ${res.status}`);
      }
      const saved = payload.data as SettingsFile;
      files[kind] = saved;
      drafts[kind] = saved.content;
      delete liveIssues[kind];
      delete liveEntries[kind];

      if (restart) {
        activateRestartGrace(6000);
        const r = await apiFetch('/api/service/control?action=restart', { method: 'POST' });
        if (!r.ok) throw new Error(await r.text());
        showToast('success', $t('xkeen_settings.saved_restarted'));
        onrestarted?.();
      } else {
        showToast('success', $t('xkeen_settings.saved'));
      }
    } catch (e: any) {
      if (e?.status === 401) return;
      showToast('error', `${$t('xkeen_settings.save_error')}: ${e?.message || e}`);
    } finally {
      saving = false;
    }
  }

  function issueText(issue: Issue): string {
    return $t(`xkeen_settings.issue.${issue.code}`, { value: issue.value ?? '' });
  }

  onMount(() => {
    load();
    return () => clearTimeout(validateTimer);
  });
</script>

<div class="card xkeen-settings-card">
  <div class="xs-head">
    <div>
      <h2 class="card-title">{$t('xkeen_settings.title')}</h2>
      <p class="xs-subtitle">{$t('xkeen_settings.subtitle')}</p>
    </div>
  </div>

  {#if loading}
    <div class="xs-skeleton">
      <Skeleton type="text-line" width="70%" />
      <Skeleton type="text-line" width="50%" />
    </div>
  {:else if loadError}
    <div class="xs-load-error" role="alert">
      <span>{$t('xkeen_settings.load_error')}: {loadError}</span>
      <Button variant="secondary" onclick={load}>{$t('xkeen_settings.retry')}</Button>
    </div>
  {:else if current}
    <SegmentedControl
      items={segments}
      bind:value={activeKind}
      ariaLabel={$t('xkeen_settings.title')}
    />

    <p class="xs-hint">{$t(`xkeen_settings.hint.${activeKind}`)}</p>

    <div class="xs-meta">
      <code class="xs-path">{current.path}</code>
      {#if !current.exists}
        <span class="xs-badge">{$t('xkeen_settings.not_exists')}</span>
      {/if}
      <span class="xs-count">{$t('xkeen_settings.entries', { n: String(entries) })}</span>
    </div>

    <label class="visually-hidden" for="xkeen-settings-editor">
      {$t(`xkeen_settings.kind.${activeKind}`)}
    </label>
    <textarea
      id="xkeen-settings-editor"
      class="xs-editor"
      class:has-errors={errorCount > 0}
      spellcheck="false"
      autocomplete="off"
      rows="12"
      value={draft}
      oninput={handleInput}></textarea>

    {#if issues.length > 0}
      <ul class="xs-issues" aria-live="polite">
        {#each issues as issue, i (i)}
          <li class="xs-issue" class:is-error={issue.severity === 'error'}>
            {#if issue.line}
              <span class="xs-line">{$t('xkeen_settings.line', { n: String(issue.line) })}</span>
            {/if}
            <span>{issueText(issue)}</span>
          </li>
        {/each}
      </ul>
    {/if}

    <div class="xs-actions">
      <Button variant="secondary" disabled={!dirty || saving} onclick={revert}>
        {$t('xkeen_settings.revert')}
      </Button>
      <Button
        variant="secondary"
        disabled={!dirty || saving || errorCount > 0}
        loading={saving}
        onclick={() => save(false)}
      >
        {$t('xkeen_settings.save')}
      </Button>
      <Button
        variant="primary"
        disabled={!dirty || saving || errorCount > 0}
        loading={saving}
        title={$t('xkeen_settings.save_restart_title')}
        onclick={() => save(true)}
      >
        {$t('xkeen_settings.save_restart')}
      </Button>
    </div>
  {/if}
</div>

<style>
  .xkeen-settings-card {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .xs-head {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: var(--spacing-3);
  }

  /* Same header as the neighbouring cards on the services page instead of
     the global compact .card-title strip. */
  .xs-head .card-title {
    display: block;
    margin: 0;
    padding: 0;
    font-size: var(--font-size-lg);
    font-weight: 700;
    color: var(--fg-primary);
  }

  .xs-subtitle {
    margin: 2px 0 0;
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }

  .xs-hint {
    margin: 0;
    color: var(--fg-muted);
    font-size: var(--font-size-sm);
  }

  /* Four file tabs do not fit a phone width; wrap instead of hiding the
     last one behind horizontal scroll. */
  .xkeen-settings-card :global(.seg) {
    flex-wrap: wrap;
    align-self: flex-start;
  }

  .xs-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .xs-load-error {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-3);
    color: var(--danger);
    font-size: var(--font-size-sm);
  }

  .xs-meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    font-size: var(--font-size-xs);
    color: var(--fg-muted);
  }

  .xs-path {
    font-family: var(--font-family-mono);
    color: var(--fg-secondary);
    word-break: break-all;
  }

  .xs-badge {
    padding: 1px var(--spacing-2);
    border-radius: var(--radius-full);
    background: var(--warning-soft);
    color: var(--warning);
  }

  .xs-count {
    margin-left: auto;
  }

  .xs-editor {
    width: 100%;
    box-sizing: border-box;
    min-height: 220px;
    resize: vertical;
    padding: var(--spacing-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-input);
    color: var(--fg-primary);
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    line-height: 1.5;
    tab-size: 2;
    scrollbar-width: thin;
    scrollbar-color: var(--scrollbar-thumb) transparent;
  }

  .xs-editor:focus {
    outline: none;
    border-color: var(--border-focus);
  }

  .xs-editor.has-errors {
    border-color: var(--danger);
  }

  .xs-issues {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    max-height: 180px;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--scrollbar-thumb) transparent;
  }

  .xs-issue {
    display: flex;
    gap: var(--spacing-2);
    font-size: var(--font-size-sm);
    color: var(--warning);
  }

  .xs-issue.is-error {
    color: var(--danger);
  }

  .xs-line {
    flex-shrink: 0;
    font-family: var(--font-family-mono);
    color: var(--fg-muted);
  }

  .xs-actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
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
