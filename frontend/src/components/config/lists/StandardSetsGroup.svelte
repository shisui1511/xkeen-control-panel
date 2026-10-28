<script lang="ts">
  import { t } from '../../../i18n';
  import type { SourceView } from '../../../lib/configGeodata';
  import SourceRow from './SourceRow.svelte';

  // Группа «Стандартные XKeen»: три подгруппы поставщиков (refilter, v2fly, zkeen), в каждой
  // два переключателя — geosite и geoip, по одному на файл. Набор включён, пока файл лежит
  // на диске или ждёт скачивания по временной записи. Включение открывает диалог записи,
  // выключение — поток удаления (onoff).
  let {
    sources,
    disabled = false,
    onenable,
    onoff,
    onopen,
    ondedup,
    onfinish
  }: {
    /** Строки источников с origin standard. */
    sources: SourceView[];
    /** XKeen не умеет пользовательские базы: переключатели неактивны. */
    disabled?: boolean;
    /** Включить набор: открывает диалог записи. */
    onenable: (source: SourceView) => void;
    /** Выключить набор; пока не передан, включённый переключатель неактивен. */
    onoff?: (source: SourceView) => void;
    onopen?: (source: SourceView) => void;
    ondedup?: (source: SourceView) => void;
    onfinish?: (source: SourceView) => void;
  } = $props();

  const uid = $props.id();
  const KINDS = ['geosite', 'geoip'];

  // Поставщики — в порядке появления в снимке, внутри — geosite, затем geoip
  const providers = $derived.by(() => {
    const out: { name: string; rows: SourceView[] }[] = [];
    for (const s of sources) {
      const name = s.provider ?? s.set_id ?? s.file;
      let p = out.find((x) => x.name === name);
      if (p === undefined) {
        p = { name, rows: [] };
        out.push(p);
      }
      p.rows.push(s);
    }
    for (const p of out) {
      p.rows.sort((a, b) => KINDS.indexOf(a.kind) - KINDS.indexOf(b.kind));
    }
    return out;
  });

  const isOn = (s: SourceView) => s.present || s.pending === true;

  function toggle(e: Event & { currentTarget: HTMLInputElement }, s: SourceView) {
    // Состояние переключателя определяет снимок, а не клик: показ возвращается к снимку
    const input = e.currentTarget;
    input.checked = isOn(s);
    if (isOn(s)) onoff?.(s);
    else onenable(s);
  }
</script>

<section class="card sg-card" aria-labelledby="{uid}-title" data-testid="geo-group-standard">
  <div class="sg-head">
    <h3 class="sg-title" id="{uid}-title">{$t('cfg.geo.group_standard')}</h3>
    <p class="sg-desc">{$t('cfg.geo.group_standard_desc')}</p>
  </div>

  <div class="sg-providers">
    {#each providers as p (p.name)}
      <fieldset class="sg-provider" data-testid="geo-provider-{p.name}">
        <legend class="sg-legend">{p.name}</legend>
        <ul class="sg-list">
          {#each p.rows as s (s.id)}
            <SourceRow source={s} {onopen} {ondedup} {onfinish}>
              {#snippet lead()}
                <label class="toggle-label sg-toggle">
                  <span class="toggle-switch">
                    <input
                      type="checkbox"
                      checked={isOn(s)}
                      disabled={disabled || (isOn(s) && !onoff)}
                      aria-label={$t('cfg.geo.switch_aria', { provider: p.name, kind: s.kind })}
                      data-testid="geo-set-switch-{s.set_id ?? s.id}"
                      onchange={(e) => toggle(e, s)}
                    />
                    <span class="toggle-slider"></span>
                  </span>
                  <span class="sg-kind">{s.kind}</span>
                </label>
              {/snippet}
            </SourceRow>
          {/each}
        </ul>
      </fieldset>
    {/each}
  </div>
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

  /* До 1024 px подгруппы идут колонкой, с 1024 px — три колонки */
  .sg-providers {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .sg-provider {
    min-width: 0;
    margin: 0;
    padding: 0;
    border: none;
  }

  .sg-legend {
    padding: 0;
    font-size: var(--font-size-base);
    font-weight: 600;
    line-height: 1.5;
    color: var(--fg-primary);
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
    font-size: var(--font-size-xs);
    font-weight: 600;
  }

  .sg-kind {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-secondary);
  }

  @media (min-width: 1024px) {
    .sg-providers {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: var(--spacing-8);
    }
  }

  @media (pointer: coarse), (max-width: 768px) {
    .sg-toggle {
      min-height: 44px;
    }
  }
</style>
