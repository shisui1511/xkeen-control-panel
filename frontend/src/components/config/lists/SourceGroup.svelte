<script lang="ts">
  import { t } from '../../../i18n';
  import Button from '../../Button.svelte';
  import Section from '../../Section.svelte';
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
    ondelete,
    onrestore,
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
    /** «Удалить источник» у своих записей и файлов без записи. */
    ondelete?: (source: SourceView) => void;
    /** «Вернуть источник» у источника с пометкой черновика. */
    onrestore?: (source: SourceView) => void;
    ondedup?: (source: SourceView) => void;
    onfinish?: (source: SourceView) => void;
  } = $props();

  const isOn = (s: SourceView) => s.entry || s.present;

  function toggle(e: Event & { currentTarget: HTMLInputElement }, s: SourceView) {
    // Состояние переключателя определяет снимок, а не клик: показ возвращается к снимку
    e.currentTarget.checked = isOn(s);
    if (isOn(s)) onoff?.(s);
    else onenable?.(s);
  }
</script>

<Section
  title={$t(kind === 'panel' ? 'cfg.geo.group_panel' : 'cfg.geo.group_own')}
  description={$t(kind === 'panel' ? 'cfg.geo.group_panel_desc' : 'cfg.geo.group_own_desc')}
  testid="geo-group-{kind}"
>
  {#snippet meta()}
    {#if kind === 'own' && onadd}
      <Button
        variant="secondary"
        class="btn-sm"
        {disabled}
        data-testid="geo-add-source"
        onclick={onadd}
      >
        <Icon name="add" size={14} />
        {$t('cfg.geo.add_source')}
      </Button>
    {/if}
  {/snippet}

  <div class="card card-rows sg-card">
    {#if sources.length === 0}
      {#if kind === 'panel'}
        <p class="card-empty" data-testid="geo-panel-empty">{$t('cfg.geo.panel_empty')}</p>
      {:else}
        <div class="card-block" data-testid="geo-own-empty">
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
            <SourceRow source={s} {onopen} {ondedup} {onfinish} {onrestore}>
              {#snippet lead()}
                <label class="toggle-label sg-toggle">
                  <span class="toggle-switch">
                    <input
                      type="checkbox"
                      role="switch"
                      checked={isOn(s)}
                      disabled={disabled ||
                        !!s.draft ||
                        (isOn(s) && !onoff) ||
                        (!isOn(s) && !onenable)}
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
              ondelete={disabled ? undefined : ondelete}
              {onrestore}
              {ondedup}
              {onfinish}
            />
          {/if}
        {/each}
      </ul>
    {/if}
  </div>
</Section>

<style>
  .sg-card {
    min-width: 0;
  }

  .sg-list {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  /* Строки набора — на всю ширину карточки, отступ от края как у строк настроек */
  .sg-list :global(.sr-row) {
    padding: var(--spacing-3) var(--card-pad);
  }

  .sg-list :global(.sr-row:first-child) {
    border-top: 0;
  }

  .sg-toggle {
    min-height: var(--btn-h);
  }

  @media (pointer: coarse), (max-width: 768px) {
    :global([data-testid^='geo-group-'] .btn) {
      min-height: 44px;
    }

    .sg-toggle {
      min-height: 44px;
    }
  }
</style>
