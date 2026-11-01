<script lang="ts">
  import { t, currentLang } from '../../../i18n';
  import Icon from '../../../lib/components/Icon.svelte';
  import Section from '../../Section.svelte';
  import { formatBytes } from '../../../lib/format';
  import { geodataView } from '../../../lib/configGeodata';

  // Файлы Mihomo (D-25): сворачиваемая секция только для чтения. Файлы *.mmdb, GeoSite и
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

<Section
  title={$t('cfg.geo.mihomo_title')}
  description={$t('cfg.geo.mihomo_desc')}
  testid="geo-mihomo-files"
>
  {#snippet meta()}
    <button
      type="button"
      class="btn btn-secondary btn-sm mc-toggle"
      aria-expanded={expanded}
      aria-controls="{uid}-body"
      data-testid="geo-mihomo-toggle"
      onclick={() => (expanded = !expanded)}
    >
      <Icon name={expanded ? 'chevron-up' : 'chevron-down'} size={14} />
      {expanded ? $t('cfg.section_hide') : $t('cfg.section_show')}
      <span class="sr-only">{$t('cfg.geo.mihomo_title')}</span>
    </button>
  {/snippet}

  {#if expanded}
    <div id="{uid}-body" class="card card-rows mc" data-testid="geo-mihomo-body">
      {#if files.length === 0}
        <p class="card-empty" data-testid="geo-mihomo-empty">{$t('cfg.geo.mihomo_empty')}</p>
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
</Section>

<style>
  .mc {
    min-width: 0;
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
    padding: 10px var(--card-pad);
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

  @media (pointer: coarse), (max-width: 768px) {
    .mc-toggle {
      min-height: 44px;
    }
  }
</style>
