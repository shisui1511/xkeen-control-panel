<script lang="ts">
  import { t } from '../../../i18n';
  import { showToast } from '../../../stores';
  import Input from '../../Input.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { handleLayerError } from '../../../lib/configLayer';
  import {
    routingView,
    saveProbe,
    routingErrorText,
    type ProbeConfig
  } from '../../../lib/configRouting';

  // Параметры проверки доступности узлов: одни на оба ядра (D-12), используются режимами
  // «Авто» и «Резерв». Запись в черновик идёт по уходу фокуса из поля и только с допустимыми
  // значениями; окончательную проверку делает сервер (probe_invalid).
  const uid = $props.id();

  const MAX_URL = 512;
  const MIN_INTERVAL = 10;
  const MAX_INTERVAL = 3600;

  let open = $state(false);

  // Записанные в черновик значения; пока представление не пришло, поля заполнены значениями по умолчанию
  const saved = $derived<ProbeConfig | null>(
    $routingView ? ($routingView.probe ?? $routingView.probe_defaults ?? null) : null
  );
  const savedUrl = $derived(saved?.url ?? '');
  const savedInterval = $derived(String(saved?.interval_s ?? ''));
  const savedTimeout = $derived(String(saved?.timeout_s ?? ''));

  // Поля — производные от записанного: ввод их переопределяет, новая запись сбрасывает к ней
  let url = $derived(savedUrl);
  let interval = $derived(savedInterval);
  let timeout = $derived(savedTimeout);
  let touched = $state({ url: false, interval: false, timeout: false });
  let saving = $state(false);
  let again = false;

  function isHttpUrl(raw: string): boolean {
    const u = raw.trim();
    if (u === '' || u.length > MAX_URL || /[\s\p{Cc}]/u.test(u)) return false;
    try {
      const p = new URL(u);
      return (
        (p.protocol === 'http:' || p.protocol === 'https:') &&
        p.hostname !== '' &&
        p.username === '' &&
        p.password === ''
      );
    } catch {
      return false;
    }
  }

  function wholeSeconds(raw: string): number | null {
    const s = raw.trim();
    return /^\d{1,6}$/.test(s) ? Number(s) : null;
  }

  const intervalNum = $derived(wholeSeconds(interval));
  const intervalValid = $derived(
    intervalNum !== null && intervalNum >= MIN_INTERVAL && intervalNum <= MAX_INTERVAL
  );
  const timeoutNum = $derived(wholeSeconds(timeout));
  // Таймаут сверяется с интервалом из поля, если он допустим, иначе с записанным
  const effectiveInterval = $derived(
    intervalValid && intervalNum !== null ? intervalNum : (saved?.interval_s ?? MAX_INTERVAL)
  );
  const urlValid = $derived(isHttpUrl(url));
  const timeoutValid = $derived(
    timeoutNum !== null && timeoutNum >= 1 && timeoutNum < effectiveInterval
  );

  const urlError = $derived(touched.url && !urlValid ? $t('cfg.groups.probe_err_url') : '');
  const intervalError = $derived(
    touched.interval && !intervalValid ? $t('cfg.groups.probe_err_interval') : ''
  );
  const timeoutError = $derived(
    touched.timeout && !timeoutValid ? $t('cfg.groups.probe_err_timeout') : ''
  );

  // В черновик уходят допустимые поля; недопустимое остаётся записанным, остальные пишутся (E4 partial)
  async function commit() {
    const s = saved;
    if (!s) return;
    if (saving) {
      again = true;
      return;
    }
    const next: ProbeConfig = {
      v: s.v,
      url: urlValid ? url.trim() : s.url,
      interval_s: intervalValid && intervalNum !== null ? intervalNum : s.interval_s,
      timeout_s: timeoutValid && timeoutNum !== null ? timeoutNum : s.timeout_s
    };
    // Новый интервал не согласуется с таймаутом: пара остаётся прежней, пока таймаут не исправят
    if (next.timeout_s >= next.interval_s) {
      next.interval_s = s.interval_s;
      next.timeout_s = s.timeout_s;
    }
    if (next.url === s.url && next.interval_s === s.interval_s && next.timeout_s === s.timeout_s) {
      return;
    }
    saving = true;
    try {
      await saveProbe(next);
    } catch (err) {
      const e = err as { code?: string; params?: Record<string, string> };
      if (e.code === 'draft_conflict' || e.code === 'apply_busy') {
        handleLayerError(err);
      } else {
        showToast('error', routingErrorText($t, e.code, e.params));
      }
    } finally {
      saving = false;
      if (again) {
        again = false;
        void commit();
      }
    }
  }

  function onField(field: 'url' | 'interval' | 'timeout') {
    touched[field] = true;
    // Изменение интервала может сделать записанный таймаут недопустимым
    if (field === 'interval') touched.timeout = true;
    void commit();
  }
</script>

<section class="card probe-card" aria-labelledby="{uid}-title" data-testid="probe-card">
  <h3 class="probe-head">
    <button
      type="button"
      class="probe-expand"
      aria-expanded={open}
      aria-controls="{uid}-body"
      data-testid="probe-toggle"
      onclick={() => (open = !open)}
    >
      <Icon name={open ? 'chevron-down' : 'chevron-right'} size={16} />
      <span id="{uid}-title">{$t('cfg.groups.probe_title')}</span>
    </button>
  </h3>

  {#if open}
    <div class="probe-body" id="{uid}-body" data-testid="probe-body">
      <p class="probe-desc">{$t('cfg.groups.probe_desc')}</p>
      <div class="probe-fields">
        <div class="probe-field" data-testid="probe-url" onchange={() => onField('url')}>
          <Input
            id="{uid}-url"
            label={$t('cfg.groups.probe_url')}
            bind:value={url}
            maxlength={MAX_URL}
            error={urlError}
          />
        </div>
        <div class="probe-field" data-testid="probe-interval" onchange={() => onField('interval')}>
          <Input
            id="{uid}-interval"
            label={$t('cfg.groups.probe_interval')}
            bind:value={interval}
            maxlength={6}
            error={intervalError}
          />
        </div>
        <div class="probe-field" data-testid="probe-timeout" onchange={() => onField('timeout')}>
          <Input
            id="{uid}-timeout"
            label={$t('cfg.groups.probe_timeout')}
            bind:value={timeout}
            maxlength={6}
            error={timeoutError}
          />
        </div>
      </div>
    </div>
  {/if}
</section>

<style>
  .probe-card {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .probe-head {
    margin: 0;
    font: inherit;
  }

  .probe-expand {
    display: inline-flex;
    align-items: center;
    gap: var(--spacing-2);
    max-width: 100%;
    min-height: 32px;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--fg-primary);
    font: inherit;
    font-size: var(--font-size-lg);
    font-weight: 600;
    line-height: 1.2;
    text-align: left;
    cursor: pointer;
  }

  .probe-expand :global(svg) {
    flex-shrink: 0;
  }

  .probe-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .probe-desc {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .probe-fields {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 160px) minmax(0, 160px);
    gap: var(--spacing-3) var(--spacing-4);
    align-items: start;
    min-width: 0;
  }

  .probe-field {
    min-width: 0;
  }

  /* Длинный адрес прокручивается внутри поля: ширина карточки не меняется */
  .probe-field :global(.input) {
    width: 100%;
    min-width: 0;
    text-overflow: ellipsis;
  }

  @media (max-width: 768px) {
    .probe-fields {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  @media (pointer: coarse), (max-width: 768px) {
    .probe-expand {
      min-height: 44px;
    }
  }
</style>
