<script lang="ts">
  import { onDestroy, tick } from 'svelte';
  import { t, tp } from '../../../i18n';
  import Button from '../../Button.svelte';
  import Icon from '../../../lib/components/Icon.svelte';
  import { applyRunning, handleLayerError } from '../../../lib/configLayer';
  import {
    previewInput,
    addPreview,
    type PreviewFile,
    type PreviewView,
    type SourceErrorPayload,
    type AddResult
  } from '../../../lib/configSources';
  import { errorFromThrown } from '../../../lib/sourceErrors';
  import AddPreview from './AddPreview.svelte';

  // Распознавание запускается без отдельной кнопки: сразу после вставки и после
  // паузы в наборе. Предпросмотр ничего не пишет в черновик — только «Добавить».
  const DEBOUNCE_MS = 600;
  // Сервер ограничивает тело запроса 1 МиБ; больший файл не читается и не отправляется
  const MAX_FILE_BYTES = 1024 * 1024;

  let text = $state('');
  let files = $state<PreviewFile[]>([]);
  let dragging = $state(false);
  let preview = $state<PreviewView | null>(null);
  let loading = $state(false);
  let error = $state<SourceErrorPayload | null>(null);
  let adding = $state(false);
  let announce = $state('');
  let inputEl = $state<HTMLTextAreaElement | null>(null);
  let fileEl = $state<HTMLInputElement | null>(null);
  let cardEl = $state<HTMLElement | null>(null);

  let timer: ReturnType<typeof setTimeout> | null = null;
  let controller: AbortController | null = null;
  // Номер последнего запроса: ответ устаревшего ввода игнорируется
  let seq = 0;
  let pasted = false;

  const hasInput = $derived(text.trim() !== '' || files.length > 0);
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
      const res = await previewInput(text, files, signal);
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

  async function addFiles(list: File[]) {
    if (list.length === 0) return;
    announce = '';
    if (list.some((f) => f.size > MAX_FILE_BYTES)) {
      // Слишком большой файл не читается и не отправляется (T-145-35)
      cancel();
      preview = null;
      loading = false;
      error = { code: 'too_large', params: { unit: 'bytes', limit: String(MAX_FILE_BYTES) } };
      return;
    }
    cancel();
    loading = true;
    const read: PreviewFile[] = [];
    for (const f of list) read.push({ name: f.name, content: await f.text() });
    files = [...files, ...read];
    void run();
  }

  function onPickFiles(e: Event) {
    const el = e.currentTarget as HTMLInputElement;
    const list = Array.from(el.files ?? []);
    el.value = '';
    void addFiles(list);
  }

  function removeFile(index: number) {
    files = files.filter((_, i) => i !== index);
    announce = '';
    cancel();
    preview = null;
    error = null;
    if (!hasInput) {
      loading = false;
      return;
    }
    void run();
  }

  // Перетаскивание: только файлы; перетаскивание текста в поле работает как обычно
  function hasFiles(e: DragEvent): boolean {
    return Array.from(e.dataTransfer?.types ?? []).includes('Files');
  }

  function onDragOver(e: DragEvent) {
    if (!hasFiles(e)) return;
    e.preventDefault();
    dragging = true;
  }

  function onDragLeave(e: DragEvent) {
    if (e.relatedTarget instanceof Node && cardEl?.contains(e.relatedTarget)) return;
    dragging = false;
  }

  function onDrop(e: DragEvent) {
    if (!hasFiles(e)) return;
    e.preventDefault();
    dragging = false;
    void addFiles(Array.from(e.dataTransfer?.files ?? []));
  }

  function onPaste() {
    // Текст в поле появится позже события paste — распознаём в обработчике input
    pasted = true;
  }

  async function clearInput(refocus = true) {
    cancel();
    text = '';
    files = [];
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

  function registerCard(node: HTMLElement) {
    cardEl = node;
    return () => {
      cardEl = null;
    };
  }

  function registerFile(node: HTMLInputElement) {
    fileEl = node;
    return () => {
      fileEl = null;
    };
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

<!-- Вся карточка — зона перетаскивания файла; клавиатурная замена — кнопка «Выбрать файл» -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<section
  class="card add-card"
  class:add-dragging={dragging}
  data-testid="nodes-add"
  {@attach registerCard}
  ondragenter={onDragOver}
  ondragover={onDragOver}
  ondragleave={onDragLeave}
  ondrop={onDrop}
>
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

  <div class="add-files-row">
    <input
      {@attach registerFile}
      type="file"
      class="add-file-input"
      accept=".conf,.yaml,.yml,.json,.txt"
      multiple
      tabindex="-1"
      aria-hidden="true"
      data-testid="nodes-add-file"
      onchange={onPickFiles}
    />
    <Button variant="secondary" data-testid="nodes-add-pick" onclick={() => fileEl?.click()}>
      <Icon name="upload" size={14} />
      {$t('cfg.nodes.btn_choose_file')}
    </Button>
    <span class="add-formats" data-testid="nodes-add-hint">
      {dragging ? $t('cfg.nodes.drop_hint') : $t('cfg.nodes.add_formats')}
    </span>
  </div>

  {#if files.length > 0}
    <ul class="add-files" data-testid="nodes-add-files">
      {#each files as f, i (i)}
        <li class="chip chip-default add-file">
          <span class="add-file-name">{f.name}</span>
          <button
            type="button"
            class="add-file-remove"
            aria-label={$t('cfg.nodes.file_remove', { name: f.name })}
            onclick={() => removeFile(i)}
          >
            <Icon name="close" size={12} />
          </button>
        </li>
      {/each}
    </ul>
  {/if}

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
    border: 1px dashed var(--border-strong);
    transition:
      border-color var(--transition-fast),
      background var(--transition-fast);
  }

  .add-card.add-dragging {
    border-color: var(--accent);
    background: var(--accent-soft);
  }

  .add-files-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--spacing-2) var(--spacing-3);
  }

  .add-file-input {
    display: none;
  }

  .add-formats {
    margin-left: auto;
    min-width: 0;
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
    overflow-wrap: anywhere;
  }

  .add-card.add-dragging .add-formats {
    color: var(--fg-primary);
    font-weight: 600;
  }

  .add-files {
    display: flex;
    flex-wrap: wrap;
    gap: var(--spacing-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .add-file {
    max-width: 100%;
  }

  .add-file-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .add-file-remove {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    padding: 0;
    border: 0;
    border-radius: var(--radius-sm);
    background: transparent;
    color: inherit;
    cursor: pointer;
  }

  .add-file-remove:hover {
    background: var(--surface-tint);
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

    .add-actions :global(.btn),
    .add-files-row :global(.btn) {
      min-height: 44px;
    }

    .add-file-remove {
      width: 44px;
      height: 44px;
    }

    .add-formats {
      margin-left: 0;
    }
  }
</style>
