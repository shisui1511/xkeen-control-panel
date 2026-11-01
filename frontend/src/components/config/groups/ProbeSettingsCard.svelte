<script lang="ts">
  import { t } from '../../../i18n';
  import { showToast } from '../../../stores';
  import { onDestroy } from 'svelte';
  import Input from '../../Input.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import Section from '../../Section.svelte';
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
  const RAW_DEBOUNCE_MS = 400;
  const MIN_SAMPLING = 1;
  const MAX_SAMPLING = 100;
  // Значение sampling, которое панель пишет в файл наблюдения, когда ключ не задан
  const DEFAULT_SAMPLING = 2;

  let open = $state(false);

  // Записанные в черновик значения; пока представление не пришло, поля заполнены значениями по умолчанию
  const saved = $derived<ProbeConfig | null>(
    $routingView ? ($routingView.probe ?? $routingView.probe_defaults ?? null) : null
  );
  const savedUrl = $derived(saved?.url ?? '');
  const savedInterval = $derived(String(saved?.interval_s ?? ''));
  const savedTimeout = $derived(String(saved?.timeout_s ?? ''));
  const savedConnectivity = $derived(saved?.connectivity ?? '');
  const savedSampling = $derived(saved?.sampling ? String(saved.sampling) : '');
  const savedRaw = $derived(
    saved?.extra && Object.keys(saved.extra).length > 0 ? JSON.stringify(saved.extra, null, 2) : ''
  );
  // Расширенные параметры Xray видны, только если ядро установлено
  const xrayInstalled = $derived($routingView?.installed?.xray === true);

  // Поля — производные от записанного: ввод их переопределяет, новая запись сбрасывает к ней
  let url = $derived(savedUrl);
  let interval = $derived(savedInterval);
  let timeout = $derived(savedTimeout);
  let connectivity = $derived(savedConnectivity);
  // Поле type=number отдаёт число, а пустое — null
  let sampling = $derived<string | number>(savedSampling);
  let raw = $derived(savedRaw);
  let touched = $state({
    url: false,
    interval: false,
    timeout: false,
    connectivity: false,
    sampling: false
  });
  // Синтаксис JSON показывается через 400 мс после ввода или сразу при уходе фокуса
  let rawChecked = $state(false);
  // Отказ сервера по ключу extra (зарезервированный ключ, размер) — текст под блоком
  let rawServerError = $state('');
  let rawTimer: ReturnType<typeof setTimeout> | undefined;
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
  type ExtraParse =
    | { ok: true; value: Record<string, unknown> | undefined }
    | { ok: false; line: string; detail: string };

  // Номер строки: парсеры отдают либо строку, либо смещение (тогда считаем строки сами)
  function errorLine(text: string, message: string): string {
    const byLine = /line (\d+)/.exec(message);
    if (byLine) return byLine[1];
    const byPos = /position (\d+)/.exec(message);
    if (byPos) return String(text.slice(0, Number(byPos[1])).split('\n').length);
    return '1';
  }

  // Пустой текст и пустой объект — extra не задаётся; не объект — ошибка
  function parseExtra(text: string): ExtraParse {
    if (text.trim() === '') return { ok: true, value: undefined };
    let v: unknown;
    try {
      v = JSON.parse(text);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      return { ok: false, line: errorLine(text, message), detail: message };
    }
    if (v === null || typeof v !== 'object' || Array.isArray(v)) {
      return { ok: false, line: '1', detail: $t('cfg.groups.probe_raw_not_object') };
    }
    const obj = v as Record<string, unknown>;
    return { ok: true, value: Object.keys(obj).length > 0 ? obj : undefined };
  }

  const urlValid = $derived(isHttpUrl(url));
  // Пустой адрес связности и пустое число замеров — ключ не задан, это допустимо
  const connectivityText = $derived(connectivity.trim());
  const connectivityValid = $derived(connectivityText === '' || isHttpUrl(connectivityText));
  const samplingText = $derived(sampling == null ? '' : String(sampling).trim());
  const samplingNum = $derived(wholeSeconds(samplingText));
  const samplingValid = $derived(
    samplingText === '' ||
      (samplingNum !== null && samplingNum >= MIN_SAMPLING && samplingNum <= MAX_SAMPLING)
  );
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
  const rawParsed = $derived(parseExtra(raw));
  const rawSyntaxError = $derived(
    rawChecked && !rawParsed.ok
      ? $t('cfg.sec.raw_syntax', { line: rawParsed.line, detail: rawParsed.detail })
      : ''
  );
  const rawError = $derived(rawSyntaxError || rawServerError);
  const connectivityError = $derived(
    touched.connectivity && !connectivityValid ? $t('cfg.groups.probe_err_connectivity') : ''
  );
  const samplingError = $derived(
    touched.sampling && !samplingValid ? $t('cfg.groups.probe_err_sampling') : ''
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
    // Пустое поле — ключ не задан; недопустимое остаётся записанным. Остальные ключи
    // pingConfig (extra) уходят как записаны: правка полей их не стирает.
    const nextConnectivity = connectivityValid ? connectivityText : (s.connectivity ?? '');
    const nextSampling = samplingValid ? (samplingNum ?? 0) : (s.sampling ?? 0);
    if (nextConnectivity !== '') next.connectivity = nextConnectivity;
    if (nextSampling !== 0) next.sampling = nextSampling;
    // Верный JSON-блок записывается, неверный не пишется: остаётся записанный
    const nextExtra = rawParsed.ok ? rawParsed.value : s.extra;
    if (nextExtra !== undefined) next.extra = nextExtra;
    // Новый интервал не согласуется с таймаутом: пара остаётся прежней, пока таймаут не исправят
    if (next.timeout_s >= next.interval_s) {
      next.interval_s = s.interval_s;
      next.timeout_s = s.timeout_s;
    }
    if (
      next.url === s.url &&
      next.interval_s === s.interval_s &&
      next.timeout_s === s.timeout_s &&
      nextConnectivity === (s.connectivity ?? '') &&
      nextSampling === (s.sampling ?? 0) &&
      JSON.stringify(nextExtra ?? null) === JSON.stringify(s.extra ?? null)
    ) {
      return;
    }
    saving = true;
    try {
      await saveProbe(next);
    } catch (err) {
      const e = err as { code?: string; params?: Record<string, string> };
      if (e.code === 'draft_conflict' || e.code === 'apply_busy') {
        handleLayerError(err);
      } else if (e.code === 'invalid_input' && e.params?.field === 'extra') {
        rawServerError = routingErrorText($t, e.code, e.params);
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

  function onRawInput() {
    rawServerError = '';
    rawChecked = false;
    clearTimeout(rawTimer);
    rawTimer = setTimeout(() => (rawChecked = true), RAW_DEBOUNCE_MS);
  }

  function onRawChange() {
    clearTimeout(rawTimer);
    rawChecked = true;
    void commit();
  }

  onDestroy(() => clearTimeout(rawTimer));

  function onField(field: 'url' | 'interval' | 'timeout' | 'connectivity' | 'sampling') {
    touched[field] = true;
    // Изменение интервала может сделать записанный таймаут недопустимым
    if (field === 'interval') touched.timeout = true;
    void commit();
  }
</script>

<Section
  title={$t('cfg.groups.probe_title')}
  description={$t('cfg.groups.probe_desc')}
  testid="probe-card"
>
  {#snippet meta()}
    <button
      type="button"
      class="btn btn-secondary btn-sm probe-toggle"
      aria-expanded={open}
      aria-controls="{uid}-body"
      data-testid="probe-toggle"
      onclick={() => (open = !open)}
    >
      <Icon name={open ? 'chevron-up' : 'chevron-down'} size={14} />
      {open ? $t('cfg.section_hide') : $t('cfg.section_show')}
      <span class="sr-only">{$t('cfg.groups.probe_title')}</span>
    </button>
  {/snippet}

  {#if open}
    <div class="card probe-body" id="{uid}-body" data-testid="probe-body">
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

      {#if xrayInstalled}
        <details class="probe-advanced" data-testid="probe-advanced">
          <summary>{$t('cfg.groups.probe_advanced')}</summary>
          <div class="probe-advanced-body">
            <div class="probe-fields probe-fields-adv">
              <div
                class="probe-field probe-mono"
                data-testid="probe-connectivity"
                onchange={() => onField('connectivity')}
              >
                <Input
                  id="{uid}-connectivity"
                  label={$t('cfg.groups.probe_connectivity')}
                  bind:value={connectivity}
                  maxlength={MAX_URL}
                  error={connectivityError}
                />
                {#if connectivityText === '' && !connectivityError}
                  <p class="probe-hint">
                    {$t('cfg.sec.default_hint', { value: $t('cfg.groups.probe_default_empty') })}
                  </p>
                {/if}
              </div>
              <div
                class="probe-field probe-mono"
                data-testid="probe-sampling"
                onchange={() => onField('sampling')}
              >
                <Input
                  id="{uid}-sampling"
                  type="number"
                  label={$t('cfg.groups.probe_sampling')}
                  bind:value={sampling}
                  error={samplingError}
                />
                {#if samplingText === '' && !samplingError}
                  <p class="probe-hint">
                    {$t('cfg.sec.default_hint', { value: String(DEFAULT_SAMPLING) })}
                  </p>
                {/if}
              </div>
            </div>
            <div class="probe-field probe-mono">
              <label class="form-label" for="{uid}-raw">{$t('cfg.groups.probe_raw')}</label>
              <textarea
                id="{uid}-raw"
                class="input probe-raw"
                class:input-error={!!rawError}
                rows="6"
                spellcheck="false"
                autocomplete="off"
                autocapitalize="off"
                data-testid="probe-raw"
                aria-invalid={rawError ? 'true' : 'false'}
                aria-describedby={rawError ? `${uid}-raw-error` : undefined}
                bind:value={raw}
                oninput={onRawInput}
                onchange={onRawChange}></textarea>
              {#if rawError}
                <span class="error-text" id="{uid}-raw-error" data-testid="probe-raw-error">
                  {rawError}
                </span>
              {/if}
            </div>
            <p class="probe-hint probe-note">{$t('cfg.groups.probe_mihomo_note')}</p>
          </div>
        </details>
      {/if}
    </div>
  {/if}
</Section>

<style>
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

  .probe-field :global(.form-group) {
    margin-bottom: 0;
  }

  /* Длинный адрес прокручивается внутри поля: ширина карточки не меняется */
  .probe-field :global(.input) {
    width: 100%;
    min-width: 0;
    text-overflow: ellipsis;
  }

  .probe-fields-adv {
    grid-template-columns: minmax(0, 1fr) minmax(0, 160px);
  }

  .probe-mono :global(.input) {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    overflow-wrap: anywhere;
  }

  .probe-raw {
    width: 100%;
    min-width: 0;
    height: auto;
    min-height: 0;
    padding: var(--spacing-2) var(--spacing-3);
    resize: vertical;
    overflow-y: auto;
    scrollbar-width: thin;
  }

  .probe-advanced {
    min-width: 0;
  }

  .probe-advanced summary {
    display: flex;
    align-items: center;
    min-height: 32px;
    font-size: var(--font-size-sm);
    font-weight: 600;
    color: var(--fg-secondary);
    cursor: pointer;
  }

  .probe-advanced-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    padding-top: var(--spacing-2);
    min-width: 0;
  }

  .probe-hint {
    margin: var(--spacing-1) 0 0;
    font-size: var(--font-size-xs);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
  }

  .probe-note {
    margin: 0;
  }

  @media (max-width: 768px) {
    .probe-fields,
    .probe-fields-adv {
      grid-template-columns: minmax(0, 1fr);
    }
  }

  @media (pointer: coarse), (max-width: 768px) {
    .probe-advanced summary,
    .probe-toggle {
      min-height: 44px;
    }
  }
</style>
