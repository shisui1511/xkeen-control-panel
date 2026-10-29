<script lang="ts">
  import { t, tp } from '../../../i18n';
  import Button from '../../Button.svelte';
  import { lookupList, type ListRef, type LookupView } from '../../../lib/configGeodata';
  import { listKind, reasonOf } from './reason';

  // Поиск «куда относится» (D-25): домен или IP → списки «файл · тег», в которых он есть.
  // Поиск идёт на сервере по всем dat каталога; интерфейс показывает первые строки и
  // «и ещё N». «Создать правило с этим списком» отдаёт список вызывающему: диалог правила
  // открывает вкладка. Сбой поиска убирает прежние результаты.
  const SHOWN = 10;

  let {
    onmakerule
  }: {
    onmakerule: (ref: ListRef) => void;
  } = $props();

  const uid = $props.id();

  let query = $state('');
  let loading = $state(false);
  let result = $state<LookupView | null>(null);
  let failure = $state('');
  // Устаревший ответ (второй поиск начат раньше, чем пришёл первый) отбрасывается
  let seq = 0;

  const q = $derived(query.trim());
  const matches = $derived(result?.matches ?? []);
  const shown = $derived(matches.slice(0, SHOWN));
  const rest = $derived(Math.max(0, matches.length - SHOWN) + (result?.more ?? 0));

  async function find() {
    if (q === '' || loading) return;
    const mine = ++seq;
    loading = true;
    failure = '';
    result = null;
    try {
      const view = await lookupList(q);
      if (mine !== seq) return;
      result = view;
    } catch (err) {
      if (mine !== seq) return;
      failure = reasonOf(err);
    } finally {
      if (mine === seq) loading = false;
    }
  }

  function onsubmit(event: SubmitEvent) {
    event.preventDefault();
    void find();
  }
</script>

<section class="card lc" aria-labelledby="{uid}-title" data-testid="geo-lookup">
  <h3 class="lc-title" id="{uid}-title">{$t('cfg.geo.lookup_title')}</h3>

  <form class="lc-form" {onsubmit}>
    <div class="form-group lc-field">
      <label class="form-label" for="{uid}-q">{$t('cfg.geo.lookup_label')}</label>
      <input
        id="{uid}-q"
        type="text"
        class="input lc-input"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        placeholder={$t('cfg.geo.lookup_placeholder')}
        data-testid="geo-lookup-input"
        bind:value={query}
      />
    </div>
    <Button
      type="submit"
      variant="secondary"
      disabled={q === ''}
      {loading}
      data-testid="geo-lookup-go"
    >
      {$t('cfg.geo.lookup_go')}
    </Button>
  </form>

  <div class="lc-area" aria-live="polite">
    {#if failure !== ''}
      <p class="lc-failed" data-testid="geo-lookup-failed">
        {$t('cfg.geo.lookup_failed', { reason: failure })}
      </p>
    {:else if result !== null}
      {#if matches.length === 0}
        <p class="lc-empty" data-testid="geo-lookup-empty">{$t('cfg.geo.lookup_empty')}</p>
      {:else}
        <ul class="lc-list" aria-label={$t('cfg.geo.lookup_title')}>
          {#each shown as m (m.file + ':' + m.tag)}
            <li class="lc-row" data-testid="geo-lookup-result">
              <span class="lc-mono">{$t('cfg.geo.lookup_row', { file: m.file, tag: m.tag })}</span>
              <Button
                variant="secondary"
                class="btn-sm"
                data-testid="geo-lookup-make-rule"
                onclick={() => onmakerule({ file: m.file, tag: m.tag, kind: listKind(m.kind) })}
              >
                {$t('cfg.geo.lookup_make_rule')}
                <span class="sr-only">{m.file} · {m.tag}</span>
              </Button>
            </li>
          {/each}
        </ul>
        {#if rest > 0}
          <p class="lc-more" data-testid="geo-lookup-more">{$tp('cfg.geo.lookup_more', rest)}</p>
        {/if}
      {/if}
    {/if}
  </div>
</section>

<style>
  .lc {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .lc-title {
    margin: 0 0 var(--spacing-2);
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    color: var(--fg-primary);
  }

  .lc-form {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .lc-form :global(.form-group) {
    margin-bottom: 0;
  }

  .lc-field {
    flex: 1 1 240px;
    min-width: 0;
  }

  .lc-input {
    width: 100%;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
  }

  .lc-area {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .lc-failed {
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--danger);
    overflow-wrap: anywhere;
  }

  .lc-empty,
  .lc-more {
    margin: 0;
    font-size: var(--font-size-base);
    color: var(--fg-secondary);
  }

  .lc-list {
    margin: 0;
    padding: 0;
    list-style: none;
    min-width: 0;
  }

  .lc-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
    padding: var(--spacing-2) 0;
    border-top: 1px solid var(--border);
    min-width: 0;
  }

  .lc-row:first-child {
    border-top: none;
  }

  .lc-mono {
    flex: 1 1 200px;
    min-width: 0;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  @media (pointer: coarse), (max-width: 768px) {
    .lc :global(.btn) {
      min-height: 44px;
    }
  }
</style>
