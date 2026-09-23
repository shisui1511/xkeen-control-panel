<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../../i18n';
  import { apiFetch, apiFetchJSON } from '../../lib/api';
  import { showConfirm, showToast } from '../../stores';
  import SegmentedControl from '../SegmentedControl.svelte';

  type Mode = 'rule' | 'global' | 'direct';
  const MODES: Mode[] = ['rule', 'global', 'direct'];

  interface Props {
    /** Called after the running core switched to another mode. */
    onchanged?: (mode: Mode) => void;
  }

  let { onchanged }: Props = $props();

  /** Mode the core actually runs in. */
  let applied = $state<Mode | null>(null);
  /** Mode shown by the control; reverted to `applied` on cancel or failure. */
  let mode = $state<Mode>('rule');
  let busy = $state(false);

  const items = $derived(MODES.map((m) => ({ value: m, label: $t(`proxies.mode.${m}`) })));

  async function load() {
    try {
      const cfg = await apiFetchJSON<{ mode?: string }>('/api/mihomo/proxy/configs');
      const m = (cfg.mode ?? '').toLowerCase();
      applied = MODES.includes(m as Mode) ? (m as Mode) : null;
      if (applied) mode = applied;
    } catch {
      applied = null;
    }
  }

  async function change(next: string) {
    const previous = applied;
    const target = next as Mode;
    if (!previous || busy || target === previous) return;

    // Global and direct affect every client on the network at once.
    if (target !== 'rule') {
      const ok = await showConfirm({
        title: $t('proxies.mode.confirm_title'),
        message: $t(`proxies.mode.confirm_${target}`),
        confirmLabel: $t('proxies.mode.confirm_btn'),
        cancelLabel: $t('app.cancel'),
        variant: 'warning'
      });
      if (!ok) {
        mode = previous;
        return;
      }
    }

    busy = true;
    try {
      const res = await apiFetch('/api/mihomo/proxy/configs', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode: target })
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      applied = target;
      showToast('success', $t('proxies.mode.changed', { mode: $t(`proxies.mode.${target}`) }));
      onchanged?.(target);
    } catch (e: any) {
      mode = previous;
      if (e?.status === 401) return;
      showToast('error', `${$t('proxies.mode.error')}: ${e?.message || e}`);
    } finally {
      busy = false;
    }
  }

  onMount(load);
</script>

{#if applied}
  <div class="mode-switch" class:is-busy={busy} title={$t('proxies.mode.hint')}>
    <SegmentedControl
      {items}
      bind:value={mode}
      ariaLabel={$t('proxies.mode.label')}
      onchange={change}
    />
  </div>
{/if}

<style>
  .mode-switch {
    display: inline-flex;
    flex-shrink: 0;
  }

  .mode-switch.is-busy {
    opacity: 0.6;
    pointer-events: none;
  }
</style>
