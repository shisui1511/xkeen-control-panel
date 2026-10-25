<script lang="ts">
  import { untrack } from 'svelte';
  import { get } from 'svelte/store';
  import { t } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Skeleton from '../../Skeleton.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import {
    fetchTakeover,
    loadRouting,
    routingView,
    type TakeoverPreview
  } from '../../../lib/configRouting';
  import PresetModal from '../rules/PresetModal.svelte';
  import { takeIntoDraft, applyFromUi } from './routingOwner';

  // Диалог взятия маршрутизации (D-01, D-02, D-43, D-44). Показывает, какие файлы routing
  // Xray уйдут в копии, какие останутся на месте и какой профиль Mihomo запомнится, и только
  // по явному «Взять маршрутизацию» пишет желание в черновик. Режим take — кнопка плашки:
  // применение идёт общей кнопкой «Применить». Режим intercept — «Применить» при группах и
  // правилах без взятия: взятие сразу применяется либо применяются только узлы. Если список
  // правил пуст, «Взять» открывает диалог пресета (D-04): он сам берёт маршрутизацию в черновике
  // вместе с выбранным пресетом либо, при закрытии без выбора, с пустым списком правил.
  let {
    open,
    mode = 'take',
    onclose
  } = $props<{
    open: boolean;
    mode?: 'take' | 'intercept';
    onclose: () => void;
  }>();

  // Разделитель, по которому текст со списком файлов режется на часть до и после списка
  const FILES_TOKEN = '\u0001files\u0001';

  let preview = $state<TakeoverPreview | null>(null);
  let loading = $state(false);
  let failed = $state(false);
  let busy = $state(false);
  // Диалог пресета лежит поверх этого: после него диалог взятия закрывается
  let presetStep = $state(false);
  // Растёт с каждым чтением: устаревший ответ не затирает более свежий
  let seq = 0;

  async function load() {
    const mine = ++seq;
    loading = true;
    failed = false;
    preview = null;
    try {
      const data = await fetchTakeover();
      if (mine === seq) preview = data;
    } catch {
      if (mine === seq) failed = true;
    } finally {
      if (mine === seq) loading = false;
    }
  }

  function reset() {
    seq++;
    preview = null;
    loading = false;
    failed = false;
    busy = false;
    presetStep = false;
  }

  $effect(() => {
    if (open) untrack(load);
    else untrack(reset);
  });

  const xray = $derived(preview?.xray ?? null);
  const mihomo = $derived(preview?.mihomo ?? null);
  const moveFiles = $derived(xray?.move ?? []);
  const leftFiles = $derived(xray?.left ?? []);
  const warnings = $derived(xray?.warnings ?? []);
  const hasSections = $derived(xray !== null || mihomo !== null);

  /** Текст до и после списка файлов; сам список выводится отдельно, моноширинным. */
  function around(key: string): [string, string] {
    const [before, after = ''] = $t(key, { files: FILES_TOKEN }).split(FILES_TOKEN);
    return [before, after];
  }

  const filesParts = $derived(around('cfg.routing.take_xray_files'));
  const leftParts = $derived(around('cfg.routing.take_left'));

  function close() {
    // Пока идёт запрос, диалог не закрывается: ответ определит, что записано в черновик
    if (busy) return;
    onclose();
  }

  /** Правил в черновике нет: представление при необходимости дочитывается. */
  async function rulesEmpty(): Promise<boolean> {
    if (get(routingView) === null) await loadRouting();
    const view = get(routingView);
    return view !== null && view.rules.length === 0;
  }

  async function confirmTake() {
    if (busy || !preview) return;
    busy = true;
    try {
      if (await rulesEmpty()) {
        presetStep = true;
        return;
      }
      const ok = await takeIntoDraft();
      if (ok && mode === 'intercept') await applyFromUi();
    } finally {
      busy = false;
    }
    // При ошибке тост уже показан и состояние прежнее: диалог закрывается в любом случае
    onclose();
  }

  function presetDone() {
    presetStep = false;
    onclose();
  }

  function nodesOnly() {
    if (busy) return;
    onclose();
    void applyFromUi();
  }
</script>

<Modal
  isOpen={open}
  title={$t('cfg.routing.take_title')}
  maxWidth="640px"
  dataTestid="take-routing-modal"
  onclose={close}
>
  <div class="tr-body" data-mode={mode}>
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div
      class="tr-scroll"
      role="region"
      aria-label={$t('cfg.routing.take_title')}
      tabindex="0"
      data-testid="take-routing-content"
    >
      {#if loading}
        <div class="tr-skeleton" aria-hidden="true">
          <Skeleton type="rect" height="20px" />
          <Skeleton type="rect" height="20px" />
          <Skeleton type="rect" height="20px" />
        </div>
      {:else if failed}
        <div class="alert alert-error tr-alert" role="alert" data-testid="take-routing-error">
          <Icon name="warning" size={16} class="tr-alert-icon" />
          <span>{$t('cfg.load_error_body')}</span>
        </div>
        <div class="tr-retry">
          <Button variant="secondary" onclick={() => void load()}>
            {$t('cfg.load_error_retry')}
          </Button>
        </div>
      {:else if hasSections}
        {#if xray}
          <section class="tr-section" data-kernel="xray">
            {#if moveFiles.length > 0}
              <p class="tr-text" data-testid="take-routing-move">
                {filesParts[0]}<span class="tr-files">{moveFiles.join(', ')}</span>{filesParts[1]}
              </p>
            {:else}
              <p class="tr-text" data-testid="take-routing-none">
                {$t('cfg.routing.take_xray_none')}
              </p>
            {/if}
            {#if leftFiles.length > 0}
              <p class="tr-text" data-testid="take-routing-left">
                {leftParts[0]}<span class="tr-files">{leftFiles.join(', ')}</span>{leftParts[1]}
              </p>
            {/if}
            {#each warnings as w (w)}
              {#if w === 'speed_balancer'}
                <div class="alert alert-warning tr-alert" data-testid="take-routing-warn-{w}">
                  <Icon name="warning" size={16} class="tr-alert-icon" />
                  <span>{$t('cfg.routing.take_warn_speed_balancer')}</span>
                </div>
              {:else if w === 'force_proxy'}
                <div class="alert alert-warning tr-alert" data-testid="take-routing-warn-{w}">
                  <Icon name="warning" size={16} class="tr-alert-icon" />
                  <span>{$t('cfg.routing.take_warn_force_proxy')}</span>
                </div>
              {/if}
            {/each}
          </section>
        {/if}
        {#if mihomo}
          <section class="tr-section" data-kernel="mihomo">
            <p class="tr-text" data-testid="take-routing-mihomo">
              {$t('cfg.routing.take_mihomo', { name: mihomo.prev_profile || 'config.yaml' })}
            </p>
          </section>
        {/if}
        <div class="alert alert-warning tr-alert" data-testid="take-routing-consequence">
          <Icon name="warning" size={16} class="tr-alert-icon" />
          <span>{$t('cfg.routing.take_consequence')}</span>
        </div>
      {/if}
    </div>

    <div class="tr-footer">
      <Button
        variant="primary"
        disabled={!preview || !hasSections}
        loading={busy}
        data-testid="take-routing-confirm"
        onclick={confirmTake}
      >
        {$t('cfg.routing.take_confirm')}
      </Button>
      {#if mode === 'intercept'}
        <Button
          variant="secondary"
          disabled={busy}
          data-testid="take-routing-nodes-only"
          onclick={nodesOnly}
        >
          {$t('cfg.routing.apply_nodes_only')}
        </Button>
      {/if}
      <Button variant="secondary" disabled={busy} data-testid="take-routing-cancel" onclick={close}>
        {$t('app.cancel')}
      </Button>
    </div>
  </div>
</Modal>

<PresetModal open={presetStep} {mode} onclose={presetDone} />

<style>
  .tr-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  /* Длинный список файлов прокручивается в теле диалога, кнопки остаются на виду */
  .tr-scroll {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
    max-height: 50dvh;
    overflow-y: auto;
    scrollbar-width: thin;
  }

  .tr-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-2);
  }

  .tr-section {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .tr-text {
    margin: 0;
    font-size: var(--font-size-base);
    line-height: 1.5;
    color: var(--fg-primary);
    overflow-wrap: anywhere;
  }

  .tr-files {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    color: var(--fg-secondary);
    overflow-wrap: anywhere;
    word-break: break-word;
  }

  .tr-alert {
    margin: 0;
    min-width: 0;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .tr-alert :global(.tr-alert-icon) {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .tr-retry {
    display: flex;
  }

  .tr-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .tr-footer :global(.btn) {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .tr-footer :global(.btn) {
      flex: 1 1 100%;
    }
  }
</style>
