<script lang="ts">
  import { t } from '../../i18n';
  import Badge from '../Badge.svelte';
  import type { RuntimeClock, RuntimeGroup } from '../../lib/configProxies';

  // Строка состояния ручной группы (UI-SPEC 1.1): чем арбитр держит группу сейчас.
  // Нет удержания — строка не рисуется. Время — по часам роутера (смещение пояса
  // из вида), а не по поясу браузера (D-08).
  interface Props {
    group: RuntimeGroup;
    clock: RuntimeClock;
  }

  let { group, clock }: Props = $props();

  const hold = $derived(group.hold);

  /** HH:MM момента unix-секунд в поясе роутера. */
  function routerTime(unix: number | undefined): string {
    if (!unix) return '--:--';
    const d = new Date((unix + (clock?.offset ?? 0)) * 1000);
    const hh = String(d.getUTCHours()).padStart(2, '0');
    const mm = String(d.getUTCMinutes()).padStart(2, '0');
    return `${hh}:${mm}`;
  }
</script>

{#if hold}
  <div class="pc-state" data-testid="proxy-group-state-{group.id}" role="status" aria-live="polite">
    {#if hold.kind === 'schedule'}
      <Badge tone="info">
        {$t('proxies.rt.sched.state_active', { time: routerTime(hold.until) })}
      </Badge>
    {/if}
  </div>
{/if}

<style>
  .pc-state {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
    margin-top: var(--spacing-2);
    font-variant-numeric: tabular-nums;
  }
</style>
