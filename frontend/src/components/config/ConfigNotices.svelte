<script lang="ts">
  import { t, tp } from '../../i18n';
  import Icon from '../../lib/components/Icon.svelte';
  import Button from '../Button.svelte';
  import { cleanLegacy } from '../../lib/configLegacy';
  import {
    visibleNotices,
    dismissNotice,
    handleLayerError,
    driftCount,
    layerSnapshot,
    selectBgReason,
    type LayerNotice
  } from '../../lib/configLayer';
  import { editorOpenRequest, showConfirm, showToast } from '../../stores';
  // Часы с шагом в минуту: текст «повтор через N мин» не застывает на момент сборки
  import { minuteClock, formatDate } from '../../lib/nodesView';

  type Shown = {
    notice: LayerNotice;
    tone: 'warning' | 'error' | 'info';
    text: string;
    // Ссылка личного кабинета провайдера (только http/https), чтобы освободить слот устройства
    cabinet?: string;
    // Идентификаторы всех слитых уведомлений: закрытие снимает их разом
    ids: string[];
    // Следы старой панели (legacy_leftovers): строки списка и кнопка «Убрать» вместо крестика
    legacy?: LegacyLeftovers;
  };

  type LegacyRow = { file: string; what: string };
  type LegacyLeftovers = {
    rows: LegacyRow[];
    more: number;
    count: number;
    filesLink: boolean;
  };

  interface Props {
    /**
     * Показывать только уведомления, чей id начинается с одного из префиксов (шаг 3 мастера —
     * `['source_']`, 149.1 D-04.3). Сводка расхождения файлов — не уведомление слоя и при
     * фильтре не показывается. Без пропа или с пустым списком — все видимые уведомления.
     */
    prefixes?: string[];
  }

  let { prefixes }: Props = $props();

  const filtered = $derived(prefixes !== undefined && prefixes.length > 0);

  const KERNEL_LABELS: Record<string, string> = { xray: 'Xray', mihomo: 'Mihomo' };

  // Провайдер освобождает слот устройства только в кабинете: для этих кодов отказ — «устройство отклонено»
  const DEVICE_REJECTED_CODES = ['all_stubs', 'http_auth'];

  /** Ссылка кабинета: разбирается как адрес и принимается только со схемой http или https. */
  function cabinetUrl(raw: string | undefined): string | undefined {
    if (!raw) return undefined;
    try {
      const u = new URL(raw);
      return u.protocol === 'http:' || u.protocol === 'https:' ? u.href : undefined;
    } catch {
      return undefined;
    }
  }

  function kernelOf(n: LayerNotice): string {
    const raw = n.kernel ?? n.params?.kernel ?? n.id.split(':')[2] ?? '';
    return KERNEL_LABELS[raw] ?? raw;
  }

  /** Минуты до повтора: от абсолютного params.retry_at, иначе params.in сервера. */
  function retryMinutes(p: Record<string, string>): number {
    const at = p.retry_at ? Date.parse(p.retry_at) : NaN;
    if (Number.isFinite(at)) return Math.ceil((at - $minuteClock.getTime()) / 60_000);
    return Number(p.in);
  }

  /** Через сколько повтор: минуты, от часа — часы. */
  function inText(minutes: number): string {
    if (!Number.isFinite(minutes) || minutes <= 0) return $t('cfg.notice.in_soon');
    if (minutes >= 60) return $t('cfg.notice.in_hours', { n: Math.round(minutes / 60) });
    return $t('cfg.notice.in_minutes', { n: Math.round(minutes) });
  }

  // Причина отказа провайдера: перевод по коду, иначе сам код
  function providerReason(code: string): string {
    const key = 'cfg.nodes.err.' + code + '.title';
    const text = $t(key);
    return text === key ? code : text;
  }

  type Described = Omit<Shown, 'ids'>;

  function describeSource(n: LayerNotice): Described | null {
    const p = n.params ?? {};
    const name = p.name ?? '';
    // Дата снимка в том же виде, что и в карточке источника
    const day = formatDate(p.date) || (p.date ?? '');
    if (n.id.startsWith('source_rejected:')) {
      const snapshot = p.date ? $t('cfg.notice.snapshot_from', { date: day }) : '';
      const code = p.code ?? '';
      const text = DEVICE_REJECTED_CODES.includes(code)
        ? $t('cfg.notice.source_rejected', { name, snapshot })
        : $t('cfg.notice.source_provider_error', { name, reason: providerReason(code), snapshot });
      return { notice: n, tone: 'warning', text, cabinet: cabinetUrl(p.web_url) };
    }
    if (n.id.startsWith('source_deferred:')) {
      return { notice: n, tone: 'warning', text: $t('cfg.notice.source_deferred', { name }) };
    }
    if (n.id.startsWith('source_network:')) {
      // Рабочий снимок упоминается, только если он был (у нового источника его нет)
      const snapshot = p.date ? ' ' + $t('cfg.notice.network_snapshot', { date: day }) : '';
      // Обновление выключено: повтора не будет, срок не обещается
      if (p.no_retry) {
        return {
          notice: n,
          tone: 'warning',
          text: $t('cfg.notice.source_network_no_retry', { name }) + snapshot
        };
      }
      return {
        notice: n,
        tone: 'warning',
        text: $t('cfg.notice.source_network', { name, in: inText(retryMinutes(p)) }) + snapshot
      };
    }
    if (n.id.startsWith('source_kernel_refused:')) {
      return {
        notice: n,
        tone: 'error',
        text: $t('cfg.notice.source_kernel_refused', {
          kernel: kernelOf(n),
          name,
          reason: p.reason ?? n.reason ?? ''
        })
      };
    }
    return null;
  }

  // Фоновые события маршрутизации (фаза 146): автоматическое взятие, сбой сверки выбора,
  // замена пропавшего узла, пропавший список. Текст собирается только интерполяцией
  function describeRouting(n: LayerNotice): Described | null {
    const p = n.params ?? {};
    if (n.id.startsWith('routing_auto_taken:')) {
      const raw = n.kernel ?? n.id.split(':')[1] ?? '';
      return {
        notice: n,
        tone: 'warning',
        text: $t('cfg.notice.routing_auto_taken', { kernel: KERNEL_LABELS[raw] ?? raw })
      };
    }
    if (n.id.startsWith('routing_reconcile:')) {
      return {
        notice: n,
        tone: 'warning',
        text: $t('cfg.routing.reconcile_failed', { group: p.group ?? '' })
      };
    }
    // Отказ фонового прохода выбора при запущенном Mihomo: причина по классу, незнакомый — failed.
    // Возврат не подтверждён (reverted = 'false') — без «Прежний узел возвращён» (D-16)
    if (n.id.startsWith('routing_select:')) {
      const reason = $t(`proxies.rt.select_bg_reason.${selectBgReason(p.reason)}`);
      const key =
        p.reverted === 'false' ? 'cfg.notice.routing_select_unknown' : 'cfg.notice.routing_select';
      return {
        notice: n,
        tone: 'error',
        text: $t(key, { group: p.group || p.gid || '', reason })
      };
    }
    if (n.id.startsWith('routing_repick:')) {
      // Замену поставить не удалось (возврат, неизвестный исход, отказ прохода): повтора не будет
      const key =
        p.failed === 'true'
          ? 'cfg.groups.repick_failed'
          : p.answered === 'true'
            ? 'cfg.groups.repick_alive'
            : 'cfg.groups.repick_none';
      let text = $t(key, { group: p.group ?? '', node: p.node ?? '' });
      // Mihomo не запущен: узлы проверялись без него, по доступности сервера (D-36)
      if (p.method === 'tls') text += ' ' + $t('cfg.groups.repick_method_tls');
      return { notice: n, tone: 'warning', text };
    }
    if (n.id.startsWith('routing_list_missing:')) {
      return {
        notice: n,
        tone: 'error',
        text: $t('cfg.rules.list_missing', { file: p.file ?? '', tag: p.tag ?? '' })
      };
    }
    return null;
  }

  // Причина сбоя менеджера geodata: перевод по коду, иначе сам код (текст сервера не доверенный:
  // он попадает в разметку только текстом)
  function geoReason(code: string | undefined): string {
    if (!code) return '';
    const key = 'cfg.geo.reason_' + code;
    const text = $t(key);
    return text === key ? code : text;
  }

  // Менеджер geodata XKeen (фаза 146.1): сбой файла, сбой запуска, повтор, уборка записи,
  // недоведённое удаление, два расписания. Текст собирается только интерполяцией
  function describeGeodata(n: LayerNotice): Described | null {
    const p = n.params ?? {};
    const file = p.file ?? '';
    if (n.id === 'geodata_update:run') {
      return {
        notice: n,
        tone: 'error',
        text: $t('cfg.geo.notice_run_failed', { reason: geoReason(p.reason) })
      };
    }
    if (n.id === 'geodata_update:retry') {
      return {
        notice: n,
        tone: 'warning',
        text: $t('cfg.geo.notice_retry_failed', { reason: geoReason(p.reason) })
      };
    }
    if (n.id.startsWith('geodata_update:')) {
      return {
        notice: n,
        tone: 'error',
        text: $t('cfg.geo.notice_file_kept', { file, reason: geoReason(p.reason) })
      };
    }
    if (n.id.startsWith('geodata_cleanup:')) {
      return { notice: n, tone: 'info', text: $t('cfg.geo.cleanup_done', { file }) };
    }
    if (n.id.startsWith('geodata_include:')) {
      return { notice: n, tone: 'warning', text: $t('cfg.geo.cleanup_stuck', { file }) };
    }
    if (n.id.startsWith('geodata_delete:')) {
      return {
        notice: n,
        tone: 'error',
        text: $t('cfg.geo.del_incomplete', { file, reason: geoReason(p.reason) })
      };
    }
    if (n.id === 'geodata_cron_two') {
      return {
        notice: n,
        tone: 'warning',
        text: $t('cfg.geo.cron_two', { schedule: p.schedule ?? '' })
      };
    }
    return null;
  }

  // Перехват DNS (фаза 148, D-28): исход без пользователя dns_outcome:<вид> — те же тексты,
  // что строка в карточке; время — локальное время роутера из params.time. Прочие dns_ —
  // событие уже случилось: общая строка со ссылкой на карточку, без сырого идентификатора
  function describeDns(n: LayerNotice): Described | null {
    if (!n.id.startsWith('dns_')) return null;
    const p = n.params ?? {};
    // Без времени фраза исхода («…, {время}.») не собирается — общая строка о событии
    if (!p.time) return { notice: n, tone: 'warning', text: $t('cfg.dns.notice_event') };
    const time = p.time;
    const kernelName = (k: string | undefined): string =>
      (k ? KERNEL_LABELS[k] : undefined) ?? $t('cfg.dns.kernel_unknown');
    switch (n.id) {
      case 'dns_outcome:timeout':
        return { notice: n, tone: 'warning', text: $t('cfg.dns.outcome_timeout', { time }) };
      case 'dns_outcome:panel_restart':
        return { notice: n, tone: 'warning', text: $t('cfg.dns.outcome_panel_restart', { time }) };
      case 'dns_outcome:probe_failed':
        return {
          notice: n,
          tone: 'warning',
          text: $t('cfg.dns.outcome_probe_failed', { reason: $t('cfg.dns.reason_probe'), time })
        };
      case 'dns_outcome:watchdog':
        return {
          notice: n,
          tone: 'warning',
          text: $t('cfg.dns.outcome_watchdog', {
            kernel: kernelName(n.kernel ?? p.kernel),
            time
          })
        };
      case 'dns_outcome:kernel_changed':
        return {
          notice: n,
          tone: 'warning',
          text: $t('cfg.dns.outcome_kernel_changed', {
            from: kernelName(p.from),
            to: kernelName(p.to),
            time
          })
        };
      default:
        return { notice: n, tone: 'warning', text: $t('cfg.dns.notice_event') };
    }
  }

  // Строк списка следов в уведомлении; остальные — строкой «и ещё n»
  const LEGACY_ROWS = 5;

  // Строки параметра сервера, разделённые переводом строки; пустые отбрасываются
  function lines(raw: string | undefined): string[] {
    return (raw ?? '').split('\n').filter((l) => l !== '');
  }

  // Файлы ядер, где старая панель оставляет блок «Свои правила»; прочие находки — её
  // собственные файлы выбора узла и фрагменты подписок в каталоге Xray
  const LEGACY_DIR = 'legacy';
  const LEGACY_RULE_FILES = ['xray/05_routing.json', 'mihomo/config.yaml'];

  // Следы старой панели в файлах ядер (фаза 151, D-04, D-05). Сервер даёт только имена файлов
  // внутри каталогов ядер (`<ядро>/<файл>`) и безопасные причины отказа, без содержимого
  function describeLegacy(n: LayerNotice): Described | null {
    const p = n.params ?? {};
    if (n.id === 'legacy_cleanup_failed') {
      return {
        notice: n,
        tone: 'warning',
        text: $t('cfg.notice.legacy_cleanup_failed', { reason: n.reason ?? p.reason ?? '' })
      };
    }
    if (n.id === 'legacy_archive_failed') {
      return {
        notice: n,
        tone: 'warning',
        text: $t('cfg.notice.legacy_archive_failed', { reason: n.reason ?? p.reason ?? '' })
      };
    }
    if (n.id !== 'legacy_leftovers') return null;
    const files = lines(p.files);
    // Без прототипа: имя файла не совпадёт со свойством объекта
    const blocked: Record<string, string | undefined> = Object.create(null);
    for (const line of lines(p.blocked)) {
      const at = line.indexOf(' — ');
      if (at > 0) blocked[line.slice(0, at)] = line.slice(at + 3);
    }
    // Сбой проверки («legacy» — архив, «<ядро>/.» — каталог ядра) — не файл ядра: «Убрать»
    // его не уберёт, в счёт файлов не входит; такие строки идут после файлов ядер и показываются
    // все (их не больше трёх), а «и ещё n» считает только файлы ядер
    const isDir = (file: string) => file === LEGACY_DIR || file.endsWith('/.');
    const kernelFiles = files.filter((f) => !isDir(f));
    const dirFindings = files.filter(isDir);
    const describeRow = (file: string) => {
      const reason = blocked[file];
      if (isDir(file)) {
        const base =
          file === LEGACY_DIR
            ? $t('cfg.notice.legacy_kind_archive')
            : $t('cfg.notice.legacy_kind_kernel_dir', {
                kernel: KERNEL_LABELS[file.slice(0, -2)] ?? file.slice(0, -2)
              });
        return {
          file: file === LEGACY_DIR ? file : file.slice(0, -1),
          what: reason ? base + '; ' + $t('cfg.notice.legacy_dir_reason', { reason }) : base
        };
      }
      let what = LEGACY_RULE_FILES.includes(file)
        ? $t('cfg.notice.legacy_kind_rules')
        : $t('cfg.notice.legacy_kind_select');
      if (reason) what += '; ' + $t('cfg.notice.legacy_blocked', { reason });
      return { file, what };
    };
    const rows = [...kernelFiles.slice(0, LEGACY_ROWS), ...dirFindings].map(describeRow);
    return {
      notice: n,
      tone: 'warning',
      text:
        kernelFiles.length > 0
          ? $tp('cfg.notice.legacy_leftovers', kernelFiles.length)
          : $t('cfg.notice.legacy_dir_only'),
      legacy: {
        rows,
        more: Math.max(0, kernelFiles.length - LEGACY_ROWS),
        count: kernelFiles.length,
        // «Файлы» показывают файлы ядер; архив там исключён, поэтому для сбоя только в архиве
        // ссылки нет
        filesLink: kernelFiles.length > 0 || dirFindings.some((f) => f !== LEGACY_DIR)
      }
    };
  }

  // Уведомления известных видов; неизвестные идентификаторы не показываем
  function describe(n: LayerNotice): Described | null {
    if (n.id === 'schema_reset') {
      return { notice: n, tone: 'warning', text: $t('cfg.notice.schema_reset') };
    }
    if (n.id === 'recovered_from_journal') {
      return { notice: n, tone: 'warning', text: $t('cfg.notice.recovered_from_journal') };
    }
    if (n.id === 'journal_recovery_failed') {
      return { notice: n, tone: 'error', text: $t('cfg.notice.journal_recovery_failed') };
    }
    if (n.id.startsWith('build_failed')) {
      const raw = n.kernel ?? n.id.split(':')[1] ?? '';
      return {
        notice: n,
        tone: 'error',
        text: $t('cfg.notice.build_failed', {
          kernel: KERNEL_LABELS[raw] ?? raw,
          reason: n.reason ?? ''
        })
      };
    }
    return (
      describeSource(n) ??
      describeRouting(n) ??
      describeGeodata(n) ??
      describeDns(n) ??
      describeLegacy(n)
    );
  }

  // Уведомления о связи и об отказе провайдера сервер ведёт по одному на ядро, а в тексте
  // ядра нет: совпадающие по виду, источнику, тексту и ссылке кабинета показываются одним,
  // закрытие снимает все слитые
  const TWIN_PREFIXES = ['source_network:', 'source_rejected:'];

  const shown = $derived.by(() => {
    const out: Shown[] = [];
    for (const n of $visibleNotices) {
      if (filtered && !prefixes?.some((p) => n.id.startsWith(p))) continue;
      const d = describe(n);
      if (d === null) continue;
      const prefix = TWIN_PREFIXES.find((p) => n.id.startsWith(p));
      const twin = prefix
        ? out.find(
            (o) =>
              o.notice.id.startsWith(prefix) &&
              o.notice.id.split(':')[1] === n.id.split(':')[1] &&
              o.text === d.text &&
              o.cabinet === d.cabinet
          )
        : undefined;
      if (twin) twin.ids.push(n.id);
      else out.push({ ...d, ids: [n.id] });
    }
    return out;
  });

  // Первый файл с расхождением в порядке списка «Файлов»: Xray, затем Mihomo, внутри — по пути
  const firstDriftPath = $derived.by(() => {
    const drifted = ($layerSnapshot?.files ?? []).filter((f) => f.state.startsWith('drift_'));
    drifted.sort(
      (a, b) =>
        Number(a.kernel !== 'xray') - Number(b.kernel !== 'xray') || a.path.localeCompare(b.path)
    );
    return drifted[0]?.path ?? null;
  });

  // Ссылка сводки ведёт в «Файлы» и выбирает первый файл с расхождением: встроенный
  // Редактор забирает запрос, когда вкладка показана
  function openFirstDrift() {
    if (firstDriftPath !== null) editorOpenRequest.set(firstDriftPath);
  }

  // «Убрать»: подтверждение, затем запрос; пока он идёт, повторное нажатие не уходит, а
  // остальные действия страницы («Применить», правки) не блокируются. Уведомление исчезает
  // само, когда перечитанный снимок больше его не содержит
  let cleaning = $state(false);

  async function cleanLeftovers(count: number) {
    if (cleaning) return;
    const ok = await showConfirm({
      title: $t('cfg.notice.legacy_confirm_title'),
      message: $tp('cfg.notice.legacy_confirm_text', count),
      confirmLabel: $t('cfg.notice.legacy_clean'),
      cancelLabel: $t('app.cancel'),
      variant: 'warning'
    });
    if (!ok || cleaning) return;
    cleaning = true;
    try {
      await cleanLegacy();
      showToast('success', $t('cfg.notice.legacy_clean_done'));
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err);
      showToast('error', $t('cfg.notice.legacy_clean_failed', { reason }), 8000);
    } finally {
      cleaning = false;
    }
  }

  async function dismiss(ids: string[]) {
    try {
      for (const id of ids) await dismissNotice(id);
    } catch (err) {
      handleLayerError(err);
    }
  }
</script>

<!-- Закрытие хранится на сервере: уведомление не вернётся после перезагрузки страницы и в
     других вкладках (D-08). Системные уведомления слоя закрываются навсегда, а
     build_failed и source_* — до перезапуска панели или пока не исчезнет их условие -->
<section class="config-notices" data-testid="config-notices">
  {#if !filtered && $driftCount > 0}
    <div class="alert alert-warning" role="alert" data-testid="config-drift-notice">
      <Icon name="warning" size={16} />
      <span class="notice-text"
        >{$t('cfg.files.drift_notice', { k: $driftCount })}
        <a
          class="notice-link"
          href="#/config/files"
          data-testid="config-drift-notice-link"
          onclick={openFirstDrift}>{$t('cfg.files.drift_notice_link')}</a
        ></span
      >
    </div>
  {/if}
  {#each shown as { notice, tone, text, cabinet, ids, legacy } (notice.id)}
    {#if legacy}
      <div class="alert alert-{tone}" role="status" data-notice={notice.id}>
        <Icon name="warning" size={16} />
        <div class="notice-text notice-body">
          <span>{text}</span>
          <ul class="legacy-files" data-testid="legacy-leftovers-files">
            {#each legacy.rows as row, i (i)}
              <li><code class="legacy-file">{row.file}</code> — {row.what}</li>
            {/each}
            {#if legacy.more > 0}
              <li>{$t('cfg.notice.legacy_more', { n: legacy.more })}</li>
            {/if}
          </ul>
          {#if legacy.count > 0}
            <span class="legacy-note">{$t('cfg.notice.legacy_backup')}</span>
          {/if}
          {#if legacy.count > 0 || legacy.filesLink}
            <span class="legacy-actions">
              {#if legacy.count > 0}
                <Button
                  variant="secondary"
                  class="tap-zone-44"
                  loading={cleaning}
                  testid="legacy-leftovers-clean"
                  onclick={() => void cleanLeftovers(legacy.count)}
                >
                  {$t('cfg.notice.legacy_clean')}
                </Button>
              {/if}
              {#if legacy.filesLink}
                <a
                  class="notice-link"
                  href="#/config/files"
                  data-testid="legacy-leftovers-files-link">{$t('cfg.files.drift_notice_link')}</a
                >
              {/if}
            </span>
          {/if}
        </div>
      </div>
    {:else}
      <div class="alert alert-{tone} alert-dismissible" role="alert" data-notice={notice.id}>
        <Icon name={tone === 'info' ? 'info' : 'warning'} size={16} />
        <span class="notice-text"
          >{text}
          {#if cabinet}
            <a
              class="notice-link"
              href={cabinet}
              target="_blank"
              rel="noopener noreferrer"
              data-testid="notice-cabinet-link">{$t('cfg.notice.cabinet_link')}</a
            >
          {/if}</span
        >
        <button
          type="button"
          class="alert-close-btn"
          aria-label={$t('app.dismiss')}
          onclick={() => void dismiss(ids)}
        >
          &times;
        </button>
      </div>
    {/if}
  {/each}
</section>

<style>
  .config-notices {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
    min-width: 0;
  }

  .config-notices:empty {
    display: none;
  }

  .config-notices .alert {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
  }

  .config-notices .alert :global(svg) {
    flex: none;
    margin-top: var(--spacing-0-5);
  }

  /* Нейтральное уведомление: accent для уведомлений закрыт, цвет — вторичный текст */
  .config-notices .alert-info {
    background: var(--bg-elevated);
    border-color: var(--border-strong);
    color: var(--fg-secondary);
  }

  .notice-text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  /* Следы старой панели: текст, список файлов, пояснение и действия столбиком */
  .notice-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .legacy-files {
    margin: 0;
    padding-left: var(--spacing-4);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1);
  }

  .legacy-file {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    overflow-wrap: anywhere;
  }

  .legacy-note {
    font-size: var(--font-size-sm);
  }

  .legacy-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-3);
  }

  .notice-link {
    color: inherit;
    font-weight: 600;
    text-decoration: underline;
    white-space: nowrap;
  }

  /* Ссылка в действиях уведомления — зона нажатия не ниже 44px на сенсорных и узких экранах */
  @media (pointer: coarse), (max-width: 768px) {
    .legacy-actions .notice-link {
      display: inline-flex;
      align-items: center;
      min-height: 44px;
    }
  }
</style>
