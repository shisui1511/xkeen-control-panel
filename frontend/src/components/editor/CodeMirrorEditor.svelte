<script lang="ts">
  import { onMount, onDestroy, untrack } from 'svelte';
  import { t as translate, currentLang, type Lang } from '../../i18n';
  import {
    EditorView,
    keymap,
    lineNumbers,
    highlightActiveLineGutter,
    highlightSpecialChars,
    drawSelection,
    dropCursor,
    rectangularSelection,
    crosshairCursor,
    highlightActiveLine
  } from '@codemirror/view';
  import { EditorState, Compartment } from '@codemirror/state';
  import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
  import { searchKeymap, highlightSelectionMatches } from '@codemirror/search';
  import {
    autocompletion,
    completionKeymap,
    closeBrackets,
    closeBracketsKeymap
  } from '@codemirror/autocomplete';
  import {
    foldGutter,
    indentOnInput,
    syntaxHighlighting,
    defaultHighlightStyle,
    HighlightStyle,
    bracketMatching,
    foldKeymap
  } from '@codemirror/language';
  import { lintKeymap, linter } from '@codemirror/lint';
  import { json, jsonParseLinter, jsonLanguage } from '@codemirror/lang-json';
  import { yaml, yamlLanguage } from '@codemirror/lang-yaml';
  import { hoverTooltip } from '@codemirror/view';
  import { tags as t } from '@lezer/highlight';

  // Schema support
  import {
    jsonSchemaLinter,
    jsonSchemaHover,
    jsonCompletion,
    stateExtensions,
    handleRefresh
  } from 'codemirror-json-schema';
  import { yamlSchemaLinter, yamlSchemaHover, yamlCompletion } from 'codemirror-json-schema/yaml';

  // Schema definitions
  import { xraySchema } from '../../schemas/xray';
  import { mihomoSchema } from '../../schemas/mihomo';
  import { localizeSchema } from '../../schemas/localize';
  import { xraySnippetSource, mihomoSnippetSource } from '../../lib/snippets';

  const customHighlightStyle = HighlightStyle.define([
    { tag: t.keyword, color: 'var(--cm-keyword)' },
    { tag: t.string, color: 'var(--cm-string)' },
    { tag: t.number, color: 'var(--cm-number)' },
    { tag: t.comment, color: 'var(--cm-comment)' },
    { tag: t.propertyName, color: 'var(--cm-property)' },
    { tag: t.variableName, color: 'var(--cm-variable)' },
    { tag: t.operator, color: 'var(--cm-operator)' },
    { tag: t.bool, color: 'var(--cm-boolean)' },
    { tag: t.null, color: 'var(--cm-null)' },
    { tag: t.bracket, color: 'var(--cm-bracket)' },
    { tag: t.className, color: 'var(--cm-variable)' },
    { tag: t.typeName, color: 'var(--cm-keyword)' }
  ]);

  let {
    content = '',
    path = '',
    expertMode = false,
    schemaEnabled = true,
    view = $bindable(null),
    onContentChange,
    onCursorChange,
    onSave
  }: {
    content: string;
    path: string;
    expertMode: boolean;
    schemaEnabled: boolean;
    view: EditorView | null;
    onContentChange: (newContent: string) => void;
    onCursorChange: (line: number, col: number, pos: number, state: EditorState) => void;
    onSave: () => void;
  } = $props();

  let editorContainer: HTMLDivElement | null = $state(null);
  const schemaCompartment = new Compartment();
  let lastPath = '';

  function getSchemaExtensions(filePath: string, expert: boolean = false, lang: Lang = 'ru') {
    if (!schemaEnabled) return [];

    const isYaml = filePath.endsWith('.yaml') || filePath.endsWith('.yml');
    const isJson = filePath.endsWith('.json');

    let schema: any = null;
    if (filePath.includes('xray') || filePath.includes('/opt/etc/xray')) {
      schema = xraySchema;
    } else if (filePath.includes('mihomo') || filePath.includes('config.yaml')) {
      schema = mihomoSchema;
    }

    if (!schema) return [];
    schema = localizeSchema(schema, lang);

    const isXray = filePath.includes('xray') || filePath.includes('/opt/etc/xray');
    const snippetSource = isXray ? xraySnippetSource : mihomoSnippetSource;

    if (isJson) {
      if (expert) {
        return [
          linter(jsonParseLinter(), { delay: 300 }),
          jsonLanguage.data.of({ autocomplete: jsonCompletion() }),
          jsonLanguage.data.of({ autocomplete: snippetSource }),
          hoverTooltip(jsonSchemaHover()),
          stateExtensions(schema)
        ];
      }
      return [
        linter(jsonParseLinter(), { delay: 300 }),
        linter(jsonSchemaLinter(), { needsRefresh: handleRefresh }),
        jsonLanguage.data.of({ autocomplete: jsonCompletion() }),
        jsonLanguage.data.of({ autocomplete: snippetSource }),
        hoverTooltip(jsonSchemaHover()),
        stateExtensions(schema)
      ];
    }

    if (isYaml) {
      if (expert) {
        return [
          yamlLanguage.data.of({ autocomplete: yamlCompletion() }),
          yamlLanguage.data.of({ autocomplete: snippetSource }),
          hoverTooltip(yamlSchemaHover()),
          stateExtensions(schema)
        ];
      }
      return [
        linter(yamlSchemaLinter(), { needsRefresh: handleRefresh }),
        yamlLanguage.data.of({ autocomplete: yamlCompletion() }),
        yamlLanguage.data.of({ autocomplete: snippetSource }),
        hoverTooltip(yamlSchemaHover()),
        stateExtensions(schema)
      ];
    }

    return [];
  }

  // Create or update the EditorState / EditorView when parameters change
  $effect(() => {
    if (!editorContainer || !path) return;

    // Track path, expertMode, schemaEnabled, language reactively
    const currentPath = path;
    const currentExpertMode = expertMode;
    const currentSchemaEnabled = schemaEnabled;
    const currentLang_ = $currentLang;

    if (view && view.dom.isConnected && lastPath === currentPath) {
      const schemaExts = getSchemaExtensions(currentPath, currentExpertMode, currentLang_);
      view.dispatch({
        effects: schemaCompartment.reconfigure(schemaExts)
      });
      return;
    }

    lastPath = currentPath;
    const lang = currentPath.endsWith('.yaml') || currentPath.endsWith('.yml') ? yaml() : json();
    const schemaExts = getSchemaExtensions(currentPath, currentExpertMode, currentLang_);

    const state = EditorState.create({
      doc: untrack(() => content),
      extensions: [
        lineNumbers(),
        highlightActiveLineGutter(),
        highlightSpecialChars(),
        history(),
        foldGutter(),
        drawSelection(),
        dropCursor(),
        EditorState.allowMultipleSelections.of(true),
        indentOnInput(),
        syntaxHighlighting(customHighlightStyle),
        bracketMatching(),
        closeBrackets(),
        autocompletion(),
        rectangularSelection(),
        crosshairCursor(),
        highlightActiveLine(),
        highlightSelectionMatches(),
        keymap.of([
          {
            key: 'Mod-s',
            run: () => {
              onSave();
              return true;
            }
          },
          ...closeBracketsKeymap,
          ...defaultKeymap,
          ...searchKeymap,
          ...historyKeymap,
          ...foldKeymap,
          ...completionKeymap,
          ...lintKeymap
        ]),
        lang,
        EditorView.lineWrapping,
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            const currentContent = update.state.doc.toString();
            onContentChange(currentContent);
          }
          if (update.selectionSet || update.docChanged) {
            const pos = update.state.selection.main.head;
            const line = update.state.doc.lineAt(pos);
            onCursorChange(line.number, pos - line.from + 1, pos, update.state);
          }
        }),
        schemaCompartment.of(schemaExts)
      ]
    });

    if (view) {
      if (view.dom.isConnected) {
        view.setState(state);
      } else {
        view.destroy();
        view = new EditorView({ state, parent: editorContainer });
      }
    } else {
      view = new EditorView({ state, parent: editorContainer });
    }
  });

  // Keep editor content in sync with external updates if path is unchanged
  $effect(() => {
    if (view && content !== view.state.doc.toString()) {
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: content }
      });
    }
  });

  let isFullscreen = $state(false);

  function toggleFullscreen() {
    isFullscreen = !isFullscreen;
    if (isFullscreen) {
      if (editorContainer?.requestFullscreen) {
        editorContainer.requestFullscreen().catch(() => {});
      } else if (document.documentElement.requestFullscreen) {
        document.documentElement.requestFullscreen().catch(() => {});
      }
    } else {
      if (document.fullscreenElement) {
        document.exitFullscreen().catch(() => {});
      }
    }
  }

  function handleFullscreenChange() {
    isFullscreen = !!document.fullscreenElement;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      // Modal.svelte's own exit-fullscreen call is asynchronous, so
      // `fullscreenchange` may not have reset `isFullscreen` yet when Escape
      // arrives here. While a dialog is open it owns Escape — yield instead
      // of double-acting on the same keypress (G-71-29).
      const target = e.target as HTMLElement | null;
      if (target?.closest?.('[role="dialog"][aria-modal="true"]')) return;
    }
    if (e.key === 'Escape' && isFullscreen) {
      isFullscreen = false;
      if (document.fullscreenElement) {
        document.exitFullscreen().catch(() => {});
      }
    }
  }

  onMount(() => {
    document.addEventListener('fullscreenchange', handleFullscreenChange);
  });

  onDestroy(() => {
    document.removeEventListener('fullscreenchange', handleFullscreenChange);
    if (view) {
      view.destroy();
      view = null;
    }
  });
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="editor-cm-wrapper" class:is-fullscreen={isFullscreen} bind:this={editorContainer}>
  <div class="editor-cm-toolbar" class:is-fullscreen={isFullscreen}>
    <button
      type="button"
      class="editor-cm-tool-btn"
      onclick={toggleFullscreen}
      title={isFullscreen ? $translate('editor.exit_fullscreen') : $translate('editor.fullscreen')}
      aria-label={isFullscreen
        ? $translate('editor.exit_fullscreen')
        : $translate('editor.fullscreen')}
    >
      {#if isFullscreen}
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          ><path
            d="M8 3v3a2 2 0 0 1-2 2H3m18 0h-3a2 2 0 0 1-2-2V3m0 18v-3a2 2 0 0 1 2-2h3M3 16h3a2 2 0 0 1 2 2v3"
          /></svg
        >
      {:else}
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"><path d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7" /></svg
        >
      {/if}
    </button>
  </div>
</div>

<style>
  /* .editor-cm-wrapper geometry is owned by global.css (unconditional
     !important rule) — keeping a second copy here risks silent drift. */
  :global(.cm-editor) {
    flex: 1 !important;
    display: flex !important;
    flex-direction: column !important;
    height: 100% !important;
    font-size: 14px;
    background: var(--cm-bg) !important;
    color: var(--fg-primary) !important;
  }
  :global(.cm-scroller) {
    flex: 1 !important;
    overflow: auto !important;
    scrollbar-width: thin;
    scrollbar-color: var(--border) transparent;
  }
  :global(.cm-gutters) {
    background: var(--cm-bg) !important;
    border-right: 1px solid var(--cm-border) !important;
    color: var(--fg-dim) !important;
  }
  :global(.cm-gutter) {
    background: var(--cm-bg) !important;
    color: var(--fg-dim) !important;
  }
  :global(.cm-activeLineGutter) {
    background-color: var(--cm-active-line) !important;
    color: var(--accent) !important;
  }
  :global(.cm-activeLine) {
    background-color: var(--cm-active-line) !important;
  }
  :global(.cm-selectionBackground) {
    background: var(--hover) !important;
  }
  :global(.cm-content) {
    font-family: var(--font-family-mono) !important;
  }
  :global(.cm-scroller::-webkit-scrollbar) {
    width: 6px;
    height: 6px;
  }
  :global(.cm-scroller::-webkit-scrollbar-track) {
    background: transparent;
  }
  :global(.cm-scroller::-webkit-scrollbar-thumb) {
    background: var(--border);
    border-radius: var(--radius-sm);
  }
  :global(.cm-scroller::-webkit-scrollbar-thumb:hover) {
    background: var(--fg-dim);
  }

  /* Schema hover tooltip (codemirror-json-schema) — themed to match the panel
     instead of the library's unstyled default, and structured for markdown
     descriptions (bold section labels, inline code, blockquote warnings). */
  :global(.cm-tooltip:has(.cm6-json-schema-hover)) {
    background: var(--bg-surface-elevated) !important;
    border: 1px solid var(--border) !important;
    border-radius: var(--radius-md) !important;
    box-shadow: var(--shadow-md) !important;
    padding: 0 !important;
    max-width: 440px;
  }
  :global(.cm6-json-schema-hover) {
    font-family: var(--font-family-sans);
    color: var(--fg-secondary);
  }
  :global(.cm6-json-schema-hover--description) {
    padding: var(--spacing-3) var(--spacing-4);
    font-size: var(--font-size-sm);
    line-height: 1.5;
  }
  :global(.cm6-json-schema-hover--description p) {
    margin: 0 0 var(--spacing-2) 0;
  }
  :global(.cm6-json-schema-hover--description p:last-child) {
    margin-bottom: 0;
  }
  :global(.cm6-json-schema-hover--description strong) {
    color: var(--fg-primary);
    font-weight: 600;
  }
  :global(.cm6-json-schema-hover--description code) {
    background: var(--code-bg);
    color: var(--code-fg);
    font-family: var(--font-family-mono);
    font-size: 0.9em;
    padding: 0.05em 0.35em;
    border-radius: var(--radius-xs);
  }
  :global(.cm6-json-schema-hover--description ul) {
    margin: var(--spacing-1) 0;
    padding-left: var(--spacing-4);
  }
  :global(.cm6-json-schema-hover--description li) {
    margin: 2px 0;
  }
  :global(.cm6-json-schema-hover--description li p) {
    margin: 0;
  }
  :global(.cm6-json-schema-hover--description blockquote) {
    margin: var(--spacing-2) 0 0 0;
    padding: var(--spacing-2) var(--spacing-3);
    border-left: 3px solid var(--warning);
    background: var(--warning-soft);
    border-radius: 0 var(--radius-xs) var(--radius-xs) 0;
    color: var(--fg-primary);
  }
  :global(.cm6-json-schema-hover--description blockquote p) {
    margin: 0;
  }
  :global(.cm6-json-schema-hover--code-wrapper) {
    border-top: 1px solid var(--border);
    padding: var(--spacing-2) var(--spacing-4);
    background: var(--bg-surface);
    border-radius: 0 0 var(--radius-md) var(--radius-md);
  }
  :global(.cm6-json-schema-hover--code) {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    color: var(--fg-dim);
  }
  :global(.cm6-json-schema-hover--code code) {
    background: transparent;
    color: var(--accent);
    padding: 0;
  }

  @media (max-width: 768px) {
    :global(.cm-editor) {
      font-size: 16px;
    }
    :global(.cm-gutters) {
      display: none !important;
    }
  }
</style>
