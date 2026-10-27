<script lang="ts">
  import { untrack } from 'svelte';
  import { t } from '../../../i18n';
  import Modal from '../../Modal.svelte';
  import Button from '../../Button.svelte';
  import Input from '../../Input.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { refetchLayerState } from '../../../lib/configLayer';
  import {
    nodeRaw,
    previewNode,
    saveNode,
    type PreviewView,
    type SourceErrorPayload
  } from '../../../lib/configSources';
  import { errorFromThrown, writeErrorText } from '../../../lib/sourceErrors';
  import AddPreview from './AddPreview.svelte';

  // Правка узла «Моих узлов» идёт по исходному тексту через тот же распознаватель, что и
  // «Добавить»: форм под протоколы нет (D-13). Полный текст с секретами открыт только здесь
  // (D-34), хранится лишь в состоянии модали и сбрасывается при закрытии.
  const DEBOUNCE_MS = 600;

  let {
    open,
    entryId,
    onclose
  }: {
    open: boolean;
    entryId: string;
    onclose: () => void;
  } = $props();

  const uid = $props.id();

  let phase = $state<'idle' | 'loading' | 'ready' | 'failed'>('idle');
  let name = $state('');
  let text = $state('');
  let preview = $state<PreviewView | null>(null);
  let pvLoading = $state(false);
  let pvError = $state<SourceErrorPayload | null>(null);
  let saving = $state(false);
  let saveError = $state('');

  let timer: ReturnType<typeof setTimeout> | null = null;
  let controller: AbortController | null = null;
  // Номер последнего запроса предпросмотра: ответ устаревшего ввода игнорируется
  let seq = 0;
  // Номер загрузки исходного текста: закрытие и повторное открытие отбрасывают старый ответ
  let loadSeq = 0;

  const hasText = $derived(text.trim() !== '');
  const canSave = $derived(
    phase === 'ready' &&
      !saving &&
      hasText &&
      !pvLoading &&
      pvError === null &&
      preview !== null &&
      (preview.my_nodes?.nodes.length ?? 0) > 0 &&
      // Повтор другой записи не сохраняется (сервер ответит invalid_request)
      preview.addable > 0
  );

  function cancelPreview() {
    seq++;
    if (timer !== null) clearTimeout(timer);
    timer = null;
    controller?.abort();
    controller = null;
  }

  async function runPreview() {
    cancelPreview();
    const mine = seq;
    if (text.trim() === '') {
      pvLoading = false;
      return;
    }
    controller = new AbortController();
    const signal = controller.signal;
    pvLoading = true;
    pvError = null;
    preview = null;
    try {
      const res = await previewNode(text, entryId, signal);
      if (mine !== seq) return;
      preview = res;
    } catch (e) {
      if (mine !== seq) return;
      pvError = errorFromThrown(e);
      // Отказ edit_single_node приходит с предпросмотром: по нему видны причины по строкам
      const data = (e as { data?: unknown })?.data;
      if (data && typeof data === 'object' && Array.isArray((data as PreviewView).errors)) {
        preview = data as PreviewView;
      }
    } finally {
      if (mine === seq) pvLoading = false;
    }
  }

  function onTextInput(e: Event) {
    text = (e.currentTarget as HTMLTextAreaElement).value;
    saveError = '';
    cancelPreview();
    preview = null;
    pvError = null;
    pvLoading = hasText;
    if (hasText) timer = setTimeout(() => void runPreview(), DEBOUNCE_MS);
  }

  async function load(id: string) {
    const mine = ++loadSeq;
    phase = 'loading';
    saveError = '';
    try {
      const raw = await nodeRaw(id);
      if (mine !== loadSeq) return;
      name = raw.name;
      text = raw.raw;
      phase = 'ready';
      void runPreview();
    } catch {
      if (mine !== loadSeq) return;
      phase = 'failed';
    }
  }

  function reset() {
    loadSeq++;
    cancelPreview();
    phase = 'idle';
    name = '';
    text = '';
    preview = null;
    pvLoading = false;
    pvError = null;
    saving = false;
    saveError = '';
  }

  $effect(() => {
    if (open) {
      const id = entryId;
      untrack(() => void load(id));
    } else {
      untrack(reset);
    }
  });

  function close() {
    // Пока идёт запись, модаль не закрывается: ответ определит, остаются ли значения
    if (saving) return;
    onclose();
  }

  async function save() {
    if (!canSave) return;
    saving = true;
    saveError = '';
    try {
      await saveNode(entryId, name.trim(), text);
      saving = false;
      onclose();
    } catch (e) {
      saveError = writeErrorText(e, $t);
      if ((e as { code?: unknown })?.code === 'draft_conflict') void refetchLayerState();
    } finally {
      saving = false;
    }
  }
</script>

<Modal
  isOpen={open}
  title={$t('cfg.nodes.edit_title')}
  maxWidth="640px"
  dataTestid="nodes-edit-modal"
  onclose={close}
>
  {#if phase === 'loading' || phase === 'idle'}
    <div class="ne-loading" data-testid="nodes-edit-loading">
      <span class="spinner" aria-hidden="true"></span>
      <span>{$t('cfg.nodes.edit_loading')}</span>
    </div>
  {:else if phase === 'failed'}
    <div class="alert alert-error ne-alert" role="alert" data-testid="nodes-edit-load-error">
      <Icon name="warning" size={16} class="ne-alert-icon" />
      <span>{$t('cfg.nodes.edit_load_error')}</span>
    </div>
    <div class="ne-footer">
      <Button variant="secondary" onclick={onclose}>{$t('cfg.nodes.cancel')}</Button>
      <Button variant="secondary" onclick={() => void load(entryId)}>{$t('cfg.nodes.retry')}</Button
      >
    </div>
  {:else}
    <div class="ne-body">
      <!-- Предел имени на сервере — 128 символов (maxNameRunes) -->
      <Input id="{uid}-name" label={$t('cfg.nodes.edit_name')} bind:value={name} maxlength={128} />

      <div class="form-group">
        <label class="form-label" for="{uid}-text">{$t('cfg.nodes.edit_text')}</label>
        <div class="alert alert-warning ne-alert" data-testid="nodes-edit-warning">
          <Icon name="warning" size={16} class="ne-alert-icon" />
          <span>{$t('cfg.nodes.edit_warning')}</span>
        </div>
        <textarea
          id="{uid}-text"
          class="input ne-text"
          rows="8"
          spellcheck="false"
          autocomplete="off"
          autocapitalize="off"
          value={text}
          data-testid="nodes-edit-text"
          oninput={onTextInput}></textarea>
      </div>

      {#if hasText}
        <AddPreview mode="single" {preview} loading={pvLoading} error={pvError} />
      {/if}

      {#if saveError}
        <div class="alert alert-error ne-alert" role="alert" data-testid="nodes-edit-error">
          <Icon name="warning" size={16} class="ne-alert-icon" />
          <span>{saveError}</span>
        </div>
      {/if}

      <div class="ne-footer">
        <Button
          variant="primary"
          disabled={!canSave}
          loading={saving}
          data-testid="nodes-edit-save"
          onclick={save}
        >
          {$t('cfg.nodes.save_draft')}
        </Button>
        <Button variant="secondary" disabled={saving} onclick={close}>
          {$t('cfg.nodes.cancel')}
        </Button>
      </div>
    </div>
  {/if}
</Modal>

<style>
  .ne-body {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .ne-loading {
    display: flex;
    align-items: center;
    gap: var(--spacing-2);
    color: var(--fg-secondary);
  }

  .ne-alert {
    margin: 0 0 var(--spacing-2);
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .ne-alert :global(.ne-alert-icon) {
    flex-shrink: 0;
    margin-top: 3px;
  }

  .ne-body :global(.form-group) {
    margin-bottom: 0;
  }

  .ne-text {
    width: 100%;
    height: auto;
    padding: var(--spacing-2) var(--spacing-3);
    min-height: 0;
    resize: vertical;
    overflow-y: auto;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    font-weight: 400;
    line-height: 1.5;
    overflow-wrap: anywhere;
    word-break: break-word;
    white-space: pre-wrap;
  }

  .ne-footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--spacing-2);
    padding-top: var(--spacing-2);
  }

  @media (pointer: coarse), (max-width: 768px) {
    .ne-footer :global(.btn) {
      min-height: 44px;
    }
  }

  @media (max-width: 480px) {
    .ne-footer :global(.btn) {
      flex: 1 1 auto;
    }
  }
</style>
