<script lang="ts">
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';
  import EmptyState from '../../EmptyState.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import type { SourceView } from '../../../lib/configGeodata';
  import SourceRow from './SourceRow.svelte';

  // Группы «Наборы панели» и «Свои». Наборы панели — каталог 146 с одним переключателем:
  // включённый набор — постоянная запись geodata. «Свои» — все остальные записи geodata
  // (в том числе вписанные в xkeen.json вручную) и файлы dat без записи.
  let {
    kind,
    sources,
    disabled = false,
    onenable,
    onoff,
    onopen,
    onadd,
    onedit,
    ondedup,
    onfinish
  }: {
    kind: 'panel' | 'own';
    sources: SourceView[];
    /** XKeen не умеет пользовательские базы: переключатели и «Добавить источник» неактивны. */
    disabled?: boolean;
    /** Включить набор панели: открывает диалог записи. */
    onenable?: (source: SourceView) => void;
    /** Выключить набор панели; пока не передан, включённый переключатель неактивен. */
    onoff?: (source: SourceView) => void;
    onopen?: (source: SourceView) => void;
    /** «Добавить источник»; пока не передан, кнопка не рисуется. */
    onadd?: () => void;
    /** «Изменить ссылку» у своих записей. */
    onedit?: (source: SourceView) => void;
    ondedup?: (source: SourceView) => void;
    onfinish?: (source: SourceView) => void;
  } = $props();

  const uid = $props.id();

  const isOn = (s: SourceView) => s.entry || s.present;

  function toggle(e: Event & { currentTarget: HTMLInputElement }, s: SourceView) {
    // Состояние переключателя определяет снимок, а не клик: показ возвращается к снимку
    e.currentTarget.checked = isOn(s);
    if (isOn(s)) onoff?.(s);
    else onenable?.(s);
  }
</script>

<section class="card sg-card" aria-labelledby="{uid}-title" data-testid="geo-group-{kind}">
  <div class="sg-head">
    <div class="sg-head-text">
      <h3 class="sg-title" id="{uid}-title">
        {$t(kind === 'panel' ? 'cfg.geo.group_panel' : 'cfg.geo.group_own')}
      </h3>
      <p class="sg-desc">
        {$t(kind === 'panel' ? 'cfg.geo.group_panel_desc' : 'cfg.geo.group_own_desc')}
      </p>
    </div>
    {#if kind === 'own' && onadd}
      <Button variant="primary" {disabled} data-testid="geo-add-source" onclick={onadd}>
        <Icon name="add" size={14} />
        {$t('cfg.geo.add_source')}
      </Button>
    {/if}
  </div>

  {#if sources.length === 0}
    {#if kind === 'panel'}
      <p class="sg-empty" data-testid="geo-panel-empty">{$t('cfg.geo.panel_empty')}</p>
    {:else}
      <div data-testid="geo-own-empty">
        <EmptyState
          title={$t('cfg.geo.own_empty_title')}
          description={$t('cfg.geo.own_empty_body')}
          ctaText={onadd ? $t('cfg.geo.add_source') : ''}
          oncta={onadd}
          plain
        />
      </div>
    {/if}
  {:else}
    <ul
      class="sg-list"
      aria-label={$t(kind === 'panel' ? 'cfg.geo.group_panel' : 'cfg.geo.group_own')}
    >
      {#each sources as s (s.id)}
        {#if kind === 'panel'}
          <SourceRow source={s} {onopen} {ondedup} {onfinish}>
            {#snippet lead()}
              <label class="toggle-label sg-toggle">
                <span class="toggle-switch">
                  <input
                    type="checkbox"
                    checked={isOn(s)}
                    disabled={disabled || (isOn(s) && !onoff) || (!isOn(s) && !onenable)}
                    aria-label={$t('cfg.geo.switch_aria', { provider: s.file, kind: s.kind })}
                    data-testid="geo-set-switch-{s.set_id ?? s.id}"
                    onchange={(e) => toggle(e, s)}
                  />
                  <span class="toggle-slider"></span>
                </span>
              </label>
            {/snippet}
          </SourceRow>
        {:else}
          <SourceRow
            source={s}
            {onopen}
            onedit={disabled ? undefined : onedit}
            {ondedup}
            {onfinish}
          />
        {/if}
      {/each}
    </ul>
  {/if}
</section>

<style>
  .sg-card {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .sg-head {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--spacing-2) var(--spacing-4);
    min-width: 0;
  }

  .sg-head-text {
    display: flex;
    flex: 1 1 240px;
    flex-direction: column;
    gap: var(--spacing-1);
    min-width: 0;
  }

  .sg-title {
    margin: 0;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    color: var(--fg-primary);
  }

  .sg-desc {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
  }

  .sg-empty {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-dim);
  }

  .sg-list {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .sg-toggle {
    min-height: var(--btn-h, 32px);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .sg-card :global(.btn) {
      min-height: 44px;
    }

    .sg-toggle {
      min-height: 44px;
    }
  }
</style>
