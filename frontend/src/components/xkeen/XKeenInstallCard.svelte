<script lang="ts">
  import { t } from '../../i18n';
  import { showConfirm, showToast } from '../../stores';
  import Button from '../Button.svelte';
  import Modal from '../Modal.svelte';
  import SegmentedControl from '../SegmentedControl.svelte';
  import Terminal from '../Terminal.svelte';

  interface Props {
    /** Панель может поставить XKeen (на роутере есть Entware) */
    available: boolean;
    /** Установщик завершился — родитель обновляет статус служб */
    onfinished?: () => void;
  }

  let { available, onfinished }: Props = $props();

  let channel = $state<'stable' | 'beta'>('stable');
  let isOpen = $state(false);
  let running = $state(false);
  let exitCode = $state<number | null>(null);

  function start() {
    exitCode = null;
    running = true;
    isOpen = true;
  }

  function handleExit(code: number) {
    running = false;
    exitCode = code;
    if (code === 0) {
      showToast('success', $t('xkinst.done'));
    } else {
      showToast('error', $t('xkinst.failed', { code }));
    }
    onfinished?.();
  }

  async function close() {
    if (running) {
      const ok = await showConfirm({
        title: $t('xkinst.abort_title'),
        message: $t('xkinst.abort_message'),
        confirmLabel: $t('xkinst.abort_confirm'),
        variant: 'warning'
      });
      if (!ok) return;
      running = false;
      onfinished?.();
    }
    isOpen = false;
  }
</script>

<div class="card install-card" data-testid="xkeen-install-card">
  <div class="install-head">
    <h2 class="card-title">{$t('xkinst.title')}</h2>
    <p class="card-subtitle">{$t('xkinst.desc')}</p>
  </div>

  {#if available}
    <div class="install-row">
      <span class="install-lbl">{$t('xkinst.channel')}</span>
      <SegmentedControl
        ariaLabel={$t('xkinst.channel')}
        value={channel}
        items={[
          { value: 'stable', label: $t('xkinst.channel_stable') },
          { value: 'beta', label: $t('xkinst.channel_beta') }
        ]}
        onchange={(v) => (channel = v === 'beta' ? 'beta' : 'stable')}
      />
      <Button variant="primary" onclick={start} data-testid="xkeen-install-start">
        {$t('xkinst.start')}
      </Button>
    </div>
    <p class="install-hint">{$t('xkinst.hint')}</p>
  {:else}
    <p class="install-hint warn">{$t('xkinst.no_entware')}</p>
  {/if}
</div>

<Modal
  {isOpen}
  title={$t('xkinst.modal_title')}
  maxWidth="960px"
  onclose={close}
  dataTestid="xkeen-install-modal"
>
  <div class="install-modal">
    {#if exitCode === null}
      <p class="install-hint">{$t('xkinst.modal_hint')}</p>
    {:else if exitCode === 0}
      <p class="install-result ok">{$t('xkinst.done')}</p>
    {:else}
      <p class="install-result fail">{$t('xkinst.failed', { code: exitCode })}</p>
    {/if}
    <div class="install-terminal">
      {#if isOpen}
        <Terminal mode="xkeen-install" {channel} onexit={handleExit} />
      {/if}
    </div>
    <div class="install-actions">
      <Button variant="secondary" onclick={close}>
        {running ? $t('xkinst.abort_confirm') : $t('app.close')}
      </Button>
    </div>
  </div>
</Modal>

<style>
  .install-card {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    border-color: var(--accent);
  }

  .install-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-3);
  }

  .install-lbl {
    color: var(--fg-secondary);
    font-size: var(--font-size-sm);
  }

  .install-hint {
    margin: 0;
    color: var(--fg-secondary);
    font-size: var(--font-size-sm);
  }

  .install-hint.warn {
    color: var(--warning);
  }

  .install-modal {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
  }

  .install-terminal {
    height: min(60vh, 520px);
    display: flex;
    flex-direction: column;
  }

  .install-terminal :global(.terminal-card) {
    flex: 1;
    min-height: 0;
  }

  .install-result {
    margin: 0;
    font-weight: 600;
  }

  .install-result.ok {
    color: var(--success);
  }

  .install-result.fail {
    color: var(--danger);
  }

  .install-actions {
    display: flex;
    justify-content: flex-end;
  }
</style>
