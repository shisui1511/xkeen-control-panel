<script lang="ts">
  import { t, currentLang } from '../../../i18n';
  import Icon from '../../../lib/components/Icon.svelte';
  import { formatBytes } from '../../../lib/format';
  import { geodataView } from '../../../lib/configGeodata';

  // Файлы Mihomo (D-25): сворачиваемая карточка только для чтения. Файлы *.mmdb, GeoSite и
  // GeoIP лежат отдельно от баз XKeen; панель их не читает и не меняет, поэтому у каждой
  // строки чип «не используется панелью», а действий нет.
  const uid = $props.id();

  let expanded = $state(false);

  const files = $derived($geodataView?.mihomo ?? []);

  /** День и месяц из RFC3339 как есть: время роутера, без пересчёта в пояс браузера. */
  function routerDate(iso: string): string {
    const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso);
    if (m === null) return iso;
    const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
    return d.toLocaleDateString($currentLang, { day: 'numeric', month: 'short' });
  }
</script>

<section class="card mc" data-testid="geo-mihomo-files">
  <h3 class="mc-title">
    <button
      type="button"
      class="mc-toggle"
      aria-expanded={expanded}
      aria-controls="{uid}-body"
      data-testid="geo-mihomo-toggle"
      onclick={() => (expanded = !expanded)}
    >
      <span>{$t('cfg.geo.mihomo_title')}</span>
      <Icon name={expanded ? 'chevron-up' : 'chevron-down'} size={16} />
    </button>
  </h3>

  {#if expanded}
    <div id="{uid}-body" class="mc-body" data-testid="geo-mihomo-body">
      <p class="mc-desc">{$t('cfg.geo.mihomo_desc')}</p>
      {#if files.length === 0}
        <p class="mc-empty" data-testid="geo-mihomo-empty">{$t('cfg.geo.mihomo_empty')}</p>
      {:else}
        <ul class="mc-list" aria-label={$t('cfg.geo.mihomo_title')}>
          {#each files as f (f.name)}
            <li class="mc-row" data-testid="geo-mihomo-file">
              <span class="mc-name">{f.name}</span>
              <span class="mc-meta">{formatBytes(f.size)} · {routerDate(f.mtime)}</span>
              <span class="chip chip-default">{$t('cfg.geo.mihomo_chip')}</span>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}
</section>

<style>
  .mc {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .mc-title {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    color: var(--fg-primary);
  }

  .mc-toggle {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
    width: 100%;
    min-height: var(--btn-h, 32px);
    padding: 0;
    border: none;
    background: none;
    font: inherit;
    color: inherit;
    text-align: left;
    cursor: pointer;
    border-radius: var(--radius-sm, 4px);
  }

  .mc-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .mc-desc,
  .mc-empty {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .mc-list {
    margin: 0;
    padding: 0;
    list-style: none;
    min-width: 0;
  }

  .mc-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-1) var(--spacing-2);
    padding: var(--spacing-2) 0;
    border-top: 1px solid var(--border);
    min-width: 0;
  }

  .mc-row:first-child {
    border-top: none;
  }

  .mc-name {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .mc-meta {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
  }
</style>
