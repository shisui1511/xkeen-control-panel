<script lang="ts">
  import { onDestroy, tick } from 'svelte';
  import { t, tp } from '../../../i18n';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { applyRunning, handleLayerError } from '../../../lib/configLayer';
  import {
    previewInput,
    addPreview,
    type PreviewView,
    type SourceErrorPayload,
    type AddResult
  } from '../../../lib/configSources';
  import { errorFromThrown } from '../../../lib/sourceErrors';
  import AddPreview from './AddPreview.svelte';

  // Распознавание запускается без отдельной кнопки: сразу после вставки и после
  // паузы в наборе. Предпросмотр ничего не пишет в черновик — только «Добавить».
  const DEBOUNCE_MS = 600;

  let text = $state('');
  let preview = $state<PreviewView | null>(null);
  let loading = $state(false);
  let error = $state<SourceErrorPayload | null>(null);
  let adding = $state(false);
  let announce = $state('');
  let inputEl = $state<HTMLTextAreaElement | null>(null);

  let timer: ReturnType<typeof setTimeout> | null = null;
  let controller: AbortController | null = null;
  // Номер последнего запроса: ответ устаревшего ввода игнорируется
  let seq = 0;
  let pasted = false;

  const hasInput = $derived(text.trim() !== '');
  const canAdd = $derived(
    !loading && !adding && preview !== null && preview.addable > 0 && !$applyRunning
  );

  function cancel() {
    seq++;
    if (timer !== null) clearTimeout(timer);
    timer = null;
    controller?.abort();
    controller = null;
  }

  async function run() {
    cancel();
    const mine = seq;
    if (!hasInput) {
      loading = false;
      return;
    }
    controller = new AbortController();
    const signal = controller.signal;
    loading = true;
    error = null;
    preview = null;
    try {
      const res = await previewInput(text, [], signal);
      if (mine !== seq) return;
      preview = res;
    } catch (e) {
      if (mine !== seq) return;
      error = errorFromThrown(e);
    } finally {
      if (mine === seq) loading = false;
    }
  }

  function onInput(e: Event) {
    text = (e.currentTarget as HTMLTextAreaElement).value;
    announce = '';
    cancel();
    preview = null;
    error = null;
    if (!hasInput) {
      loading = false;
      pasted = false;
      return;
    }
    // Старый результат скрыт до нового ответа: «Добавить» не должна класть устаревший токен
    loading = true;
    if (pasted) {
      pasted = false;
      void run();
    } else {
      timer = setTimeout(() => void run(), DEBOUNCE_MS);
    }
  }

  function onPaste() {
    // Текст в поле появится позже события paste — распознаём в обработчике input
    pasted = true;
  }

  async function clearInput(refocus = true) {
    cancel();
    text = '';
    preview = null;
    error = null;
    loading = false;
    pasted = false;
    if (refocus) {
      await tick();
      inputEl?.focus();
    }
  }

  function onClear() {
    announce = '';
    void clearInput();
  }

  function summaryOf(res: AddResult): string {
    const parts: string[] = [];
    if (res.subscriptions > 0) parts.push($tp('cfg.nodes.added_subs', res.subscriptions));
    if (res.nodes > 0) parts.push($tp('cfg.nodes.added_nodes', res.nodes));
    return parts.join(', ');
  }

  async function add() {
    if (!canAdd || preview === null) return;
    adding = true;
    try {
      const res = await addPreview(preview.token);
      await clearInput();
      announce = $t('cfg.nodes.added_toast', { summary: summaryOf(res) });
    } catch (e) {
      const code = (e as { code?: unknown })?.code;
      if (code === 'preview_expired') {
        // Токен живёт 10 минут: распознаём тот же ввод заново, без сообщения об ошибке
        void run();
      } else {
        handleLayerError(e);
      }
    } finally {
      adding = false;
    }
  }

  function registerInput(node: HTMLTextAreaElement) {
    inputEl = node;
    return () => {
      inputEl = null;
    };
  }

  // Высота поля растёт с содержимым от 3 до 8 строк, дальше — внутренняя прокрутка
  $effect(() => {
    void text;
    const el = inputEl;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${el.scrollHeight}px`;
  });

  onDestroy(cancel);
</script>

<section class="card add-card" data-testid="nodes-add">
  <label class="add-label" for="nodes-add-input">{$t('cfg.nodes.add_title')}</label>
  <textarea
    id="nodes-add-input"
    class="input add-input"
    rows="3"
    spellcheck="false"
    autocomplete="off"
    autocapitalize="off"
    placeholder={$t('cfg.nodes.add_placeholder')}
    value={text}
    {@attach registerInput}
    oninput={onInput}
    onpaste={onPaste}></textarea>

  {#if hasInput}
    <AddPreview {preview} {loading} {error} />
  {/if}

  <div class="add-actions">
    <Button
      variant="primary"
      disabled={!canAdd}
      title={$applyRunning ? $t('cfg.nodes.apply_running') : undefined}
      data-testid="nodes-add-submit"
      onclick={() => void add()}
    >
      {$t('cfg.nodes.add_btn')}
    </Button>
    <Button
      variant="secondary"
      disabled={!hasInput && !adding}
      data-testid="nodes-add-clear"
      onclick={onClear}
    >
      {$t('cfg.nodes.btn_clear')}
    </Button>
  </div>

  <p class="add-done" aria-live="polite" data-testid="nodes-add-announce">
    {#if announce}
      <Icon name="check" size={14} />
      <span>{announce}</span>
    {/if}
  </p>
</section>

<style>
  .add-card {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-3);
    min-width: 0;
  }

  .add-label {
    font-size: var(--font-size-xs);
    font-weight: 600;
    color: var(--fg-secondary);
  }

  .add-input {
    --add-line: 18px;
    height: auto;
    min-height: calc(var(--add-line) * 3 + 18px);
    max-height: calc(var(--add-line) * 8 + 18px);
    padding: 8px 12px;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    line-height: var(--add-line);
    overflow-wrap: anywhere;
    resize: none;
    overflow-y: auto;
  }

  .add-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2);
  }

  .add-done {
    display: flex;
    align-items: center;
    gap: var(--spacing-1);
    margin: 0;
    min-height: 0;
    font-size: var(--font-size-xs);
    color: var(--success);
  }

  .add-done:empty {
    display: none;
  }

  @media (max-width: 768px) {
    .add-input {
      --add-line: 24px;
      font-size: 16px;
    }

    .add-actions :global(.btn) {
      min-height: 44px;
    }
  }
</style>
