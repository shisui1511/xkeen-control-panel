<script lang="ts">
  import { t } from '../../i18n';
  import Button from '../Button.svelte';
  import Select from '../Select.svelte';
  import {
    PROBING_STRATEGIES,
    type ObservatorySettings,
    type XrayBalancer
  } from '../../lib/constructors/xrayRouting';

  interface Props {
    balancers: XrayBalancer[];
    observatorySettings: ObservatorySettings;
    outboundTags: string[];
    onchange?: () => void;
  }

  let {
    balancers = $bindable([]),
    observatorySettings = $bindable(),
    outboundTags,
    onchange
  }: Props = $props();

  const strategyOptions = $derived([
    { value: 'random', label: $t('xray.strategy_random') },
    { value: 'roundRobin', label: $t('xray.strategy_round_robin') },
    { value: 'leastPing', label: $t('xray.strategy_least_ping') },
    { value: 'leastLoad', label: $t('xray.strategy_least_load') }
  ]);
  const fallbackOptions = $derived([
    { value: '', label: $t('xray.fallback_none') },
    // outboundTags may repeat a tag; Select keys options by value.
    ...outboundTags
      .filter((tag, i, all) => tag && all.indexOf(tag) === i)
      .map((tag) => ({ value: tag, label: tag }))
  ]);
  const needsObservatory = $derived(
    balancers.some((b) => PROBING_STRATEGIES.has(b.strategy?.type ?? ''))
  );

  function matched(selector: string[]): string[] {
    const prefixes = selector.filter(Boolean);
    if (prefixes.length === 0) return [];
    return outboundTags.filter((tag) => prefixes.some((p) => tag.startsWith(p)));
  }

  function add() {
    let n = balancers.length + 1;
    while (balancers.some((b) => b.tag === `balancer-${n}`)) n++;
    balancers.push({ tag: `balancer-${n}`, selector: [], strategy: { type: 'leastPing' } });
    onchange?.();
  }

  function remove(i: number) {
    balancers.splice(i, 1);
    onchange?.();
  }

  function setSelector(b: XrayBalancer, value: string) {
    b.selector = value
      .split(/[\s,]+/)
      .map((x) => x.trim())
      .filter(Boolean);
    onchange?.();
  }

  function setStrategy(b: XrayBalancer, type: string) {
    b.strategy = { ...(b.strategy ?? {}), type };
    onchange?.();
  }
</script>

<section class="balancers" aria-labelledby="balancers-title">
  <div class="bal-head">
    <h3 id="balancers-title" class="bal-title">{$t('xray.balancers')}</h3>
    <Button variant="secondary" class="btn-sm" onclick={add}>{$t('xray.balancer_add')}</Button>
  </div>
  <p class="bal-hint">{$t('xray.balancers_hint')}</p>

  {#each balancers as b, i (i)}
    <div class="bal-card" data-testid="balancer-card">
      <div class="bal-grid">
        <div class="bal-field">
          <label class="form-label" for="bal-tag-{i}">{$t('xray.balancer_tag')}</label>
          <input
            id="bal-tag-{i}"
            class="form-input font-mono"
            bind:value={b.tag}
            oninput={() => onchange?.()}
          />
        </div>
        <div class="bal-field">
          <label class="form-label" for="bal-strategy-{i}">{$t('xray.balancer_strategy')}</label>
          <Select
            id="bal-strategy-{i}"
            value={b.strategy?.type ?? 'random'}
            options={strategyOptions}
            onchange={(e) => setStrategy(b, (e.currentTarget as HTMLSelectElement).value)}
          />
        </div>
        <div class="bal-field bal-wide">
          <label class="form-label" for="bal-selector-{i}">{$t('xray.balancer_selector')}</label>
          <input
            id="bal-selector-{i}"
            class="form-input font-mono"
            value={b.selector.join(', ')}
            placeholder="vless-, trojan-"
            onchange={(e) => setSelector(b, (e.currentTarget as HTMLInputElement).value)}
          />
          <span class="bal-matched">
            {#if matched(b.selector).length > 0}
              {$t('xray.balancer_matched', { list: matched(b.selector).join(', ') })}
            {:else}
              {$t('xray.balancer_matched_none')}
            {/if}
          </span>
        </div>
        <div class="bal-field">
          <label class="form-label" for="bal-fallback-{i}">{$t('xray.balancer_fallback')}</label>
          <Select
            id="bal-fallback-{i}"
            bind:value={b.fallbackTag}
            options={fallbackOptions}
            onchange={() => onchange?.()}
          />
        </div>
        <div class="bal-field bal-remove">
          <Button variant="danger" class="btn-sm" onclick={() => remove(i)}>
            {$t('app.delete')}
          </Button>
        </div>
      </div>
    </div>
  {/each}

  {#if needsObservatory}
    <div class="bal-card bal-observatory">
      <p class="bal-hint">{$t('xray.observatory_hint')}</p>
      <div class="bal-grid">
        <div class="bal-field bal-wide">
          <label class="form-label" for="obs-url">{$t('xray.observatory_url')}</label>
          <input
            id="obs-url"
            class="form-input font-mono"
            bind:value={observatorySettings.probeUrl}
            oninput={() => onchange?.()}
          />
        </div>
        <div class="bal-field">
          <label class="form-label" for="obs-interval">{$t('xray.observatory_interval')}</label>
          <input
            id="obs-interval"
            class="form-input font-mono"
            bind:value={observatorySettings.probeInterval}
            placeholder="1m"
            oninput={() => onchange?.()}
          />
        </div>
      </div>
    </div>
  {/if}
</section>

<style>
  .balancers {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    margin-top: var(--spacing-4);
  }

  .bal-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
  }

  .bal-title {
    margin: 0;
    font-size: var(--font-size-base);
    font-weight: 600;
    color: var(--fg-primary);
  }

  .bal-hint {
    margin: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-muted);
  }

  .bal-card {
    padding: var(--spacing-3);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-card-subtle);
  }

  .bal-observatory {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .bal-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--spacing-3);
  }

  @media (max-width: 560px) {
    .bal-grid {
      grid-template-columns: 1fr;
    }
  }

  .bal-field {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .bal-wide {
    grid-column: 1 / -1;
  }

  .bal-matched {
    font-size: var(--font-size-xs);
    color: var(--fg-muted);
    word-break: break-word;
  }

  .bal-remove {
    justify-content: flex-end;
    align-items: flex-end;
  }
</style>
