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
  import { createSchemaHoverOptions, createEnhancedTooltip } from './schemaTooltip';

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

    const hoverOpts = createSchemaHoverOptions(lang);
    const jsonHover = jsonSchemaHover(hoverOpts);
    const yamlHover = yamlSchemaHover(hoverOpts);

    const enhancedJsonHover = async (v: EditorView, pos: number, side: -1 | 1) => {
      const tt = await jsonHover(v, pos, side);
      return createEnhancedTooltip(tt, v, pos);
    };

    const enhancedYamlHover = async (v: EditorView, pos: number, side: -1 | 1) => {
      const tt = await yamlHover(v, pos, side);
      return createEnhancedTooltip(tt, v, pos);
    };

    if (isJson) {
      if (expert) {
        return [
          linter(jsonParseLinter(), { delay: 300 }),
          jsonLanguage.data.of({ autocomplete: jsonCompletion() }),
          jsonLanguage.data.of({ autocomplete: snippetSource }),
          hoverTooltip(enhancedJsonHover),
          stateExtensions(schema)
        ];
      }
      return [
        linter(jsonParseLinter(), { delay: 300 }),
        linter(jsonSchemaLinter(), { needsRefresh: handleRefresh }),
        jsonLanguage.data.of({ autocomplete: jsonCompletion() }),
        jsonLanguage.data.of({ autocomplete: snippetSource }),
        hoverTooltip(enhancedJsonHover),
        stateExtensions(schema)
      ];
    }

    if (isYaml) {
      if (expert) {
        return [
          yamlLanguage.data.of({ autocomplete: yamlCompletion() }),
          yamlLanguage.data.of({ autocomplete: snippetSource }),
          hoverTooltip(enhancedYamlHover),
          stateExtensions(schema)
        ];
      }
      return [
        linter(yamlSchemaLinter(), { needsRefresh: handleRefresh }),
        yamlLanguage.data.of({ autocomplete: yamlCompletion() }),
        yamlLanguage.data.of({ autocomplete: snippetSource }),
        hoverTooltip(enhancedYamlHover),
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

  /* Schema hover tooltip — modern IDE-grade layout matching Keenetic design system */
  :global(.cm-tooltip:has(.cm-schema-tooltip)),
  :global(.cm-tooltip:has(.cm6-json-schema-hover)) {
    background: var(--bg-surface-elevated) !important;
    border: 1px solid var(--border-strong) !important;
    border-radius: var(--radius-lg, 10px) !important;
    box-shadow:
      var(--shadow-lg, 0 10px 25px -5px rgba(0, 0, 0, 0.5)),
      0 0 0 1px var(--border) !important;
    padding: 0 !important;
    min-width: 320px;
    max-width: min(580px, calc(100vw - 32px)) !important;
    max-height: min(480px, 75vh) !important;
    overflow: hidden !important;
    backdrop-filter: blur(12px) !important;
    z-index: 100 !important;
  }

  /* Tooltip arrow integration */
  :global(.cm-tooltip.cm-tooltip-above > .cm-tooltip-arrow:after) {
    border-top-color: var(--bg-surface-elevated) !important;
  }
  :global(.cm-tooltip.cm-tooltip-below > .cm-tooltip-arrow:after) {
    border-bottom-color: var(--bg-surface) !important;
  }
  :global(.cm-tooltip.cm-tooltip-above > .cm-tooltip-arrow:before) {
    border-top-color: var(--border-strong) !important;
  }
  :global(.cm-tooltip.cm-tooltip-below > .cm-tooltip-arrow:before) {
    border-bottom-color: var(--border-strong) !important;
  }

  /* Root container */
  :global(.cm-schema-tooltip) {
    display: flex;
    flex-direction: column;
    max-height: inherit;
    font-family: var(--font-family-sans);
    color: var(--fg-secondary);
  }

  /* 1. Header */
  :global(.cm-schema-tooltip-header) {
    padding: var(--spacing-3) var(--spacing-4);
    background: var(--bg-surface);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }
  :global(.cm-schema-tooltip-title-row) {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--spacing-2);
    flex-wrap: wrap;
  }
  :global(.cm-schema-tooltip-name-group) {
    display: flex;
    align-items: baseline;
    gap: 3px;
    font-family: var(--font-family-mono);
    font-size: var(--font-size-sm);
    min-width: 0;
  }
  :global(.cm-schema-tooltip-path) {
    color: var(--fg-dim);
    font-size: var(--font-size-xs);
    opacity: 0.8;
  }
  :global(.cm-schema-tooltip-name) {
    color: var(--fg-primary);
    font-weight: 700;
    letter-spacing: -0.01em;
  }
  :global(.cm-schema-tooltip-badges) {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  :global(.cm-schema-badge-type) {
    font-family: var(--font-family-mono);
    font-size: 11px;
    font-weight: 600;
    padding: 1px 7px;
    border-radius: var(--radius-full, 9999px);
    background: color-mix(in srgb, var(--accent) 15%, transparent);
    color: var(--accent);
    border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
  }
  :global(.cm-schema-badge-required) {
    font-size: 10.5px;
    font-weight: 600;
    padding: 1px 6px;
    border-radius: var(--radius-full, 9999px);
    background: color-mix(in srgb, var(--danger, #f4707f) 15%, transparent);
    color: var(--danger, #f4707f);
    border: 1px solid color-mix(in srgb, var(--danger, #f4707f) 30%, transparent);
  }
  :global(.cm-schema-badge-default) {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    padding: 1px 7px;
    border-radius: var(--radius-full, 9999px);
    background: var(--bg-surface-elevated);
    border: 1px solid var(--border);
    color: var(--fg-secondary);
  }
  :global(.cm-schema-badge-default code) {
    color: var(--fg-primary);
    font-family: var(--font-family-mono);
    font-weight: 600;
  }

  /* 2. Body */
  :global(.cm-schema-tooltip-body) {
    padding: var(--spacing-3-5) var(--spacing-4);
    font-size: var(--font-size-sm);
    line-height: 1.6;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--border-strong) transparent;
  }
  :global(.cm-schema-tooltip-body::-webkit-scrollbar) {
    width: 6px;
  }
  :global(.cm-schema-tooltip-body::-webkit-scrollbar-track) {
    background: transparent;
  }
  :global(.cm-schema-tooltip-body::-webkit-scrollbar-thumb) {
    background: var(--border-strong);
    border-radius: var(--radius-sm);
  }
  :global(.cm-schema-tooltip-description p) {
    margin: 0 0 var(--spacing-2) 0;
  }
  :global(.cm-schema-tooltip-description p:last-child) {
    margin-bottom: 0;
  }
  :global(.cm-schema-tooltip-description strong) {
    color: var(--fg-primary);
    font-weight: 600;
  }
  :global(.cm-schema-tooltip-description code) {
    background: var(--bg-surface);
    color: var(--accent);
    border: 1px solid var(--border);
    font-family: var(--font-family-mono);
    font-size: 0.9em;
    padding: 1px 5px;
    border-radius: var(--radius-xs);
  }

  /* Option cards for bullet lists */
  :global(.cm-schema-tooltip-description ul) {
    list-style: none;
    padding: 0;
    margin: var(--spacing-2) 0;
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1-5);
  }
  :global(.cm-schema-tooltip-description li) {
    position: relative;
    padding: var(--spacing-2) var(--spacing-3);
    background: var(--bg-surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    font-size: var(--font-size-xs, 12px);
    line-height: 1.5;
    transition:
      border-color var(--transition-fast, 150ms ease),
      background var(--transition-fast, 150ms ease);
  }
  :global(.cm-schema-tooltip-description li:hover) {
    border-color: var(--border-strong);
    background: var(--bg-surface-elevated);
  }
  :global(.cm-schema-tooltip-description li p) {
    margin: 0;
  }
  :global(.cm-schema-tooltip-description li strong) {
    color: var(--accent);
    font-family: var(--font-family-mono);
    font-size: 11.5px;
    background: color-mix(in srgb, var(--accent) 10%, transparent);
    padding: 1px 5px;
    border-radius: var(--radius-xs);
    display: inline-block;
    margin-right: 4px;
  }

  /* Warning / tip callouts for blockquotes */
  :global(.cm-schema-tooltip-description blockquote) {
    margin: var(--spacing-3) 0 var(--spacing-1) 0;
    padding: var(--spacing-2-5) var(--spacing-3-5);
    background: color-mix(in srgb, var(--warning) 12%, var(--bg-surface));
    border: 1px solid color-mix(in srgb, var(--warning) 30%, transparent);
    border-left: 3px solid var(--warning);
    border-radius: var(--radius-sm);
    color: var(--fg-primary);
    font-size: var(--font-size-xs, 12px);
    line-height: 1.5;
  }
  :global(.cm-schema-tooltip-description blockquote p) {
    margin: 0;
  }

  /* 3. Enum Pills Section */
  :global(.cm-schema-tooltip-enums) {
    padding: var(--spacing-2-5) var(--spacing-4);
    background: color-mix(in srgb, var(--bg-surface) 60%, transparent);
    border-top: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: var(--spacing-1-5);
    flex-shrink: 0;
  }
  :global(.cm-schema-tooltip-enums-header) {
    display: flex;
    align-items: center;
    gap: var(--spacing-1-5);
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--fg-dim);
  }
  :global(.cm-schema-enums-icon) {
    color: var(--accent);
    flex-shrink: 0;
  }
  :global(.cm-schema-tooltip-enums-list) {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  :global(.cm-schema-enum-pill) {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-family: var(--font-family-mono);
    font-size: 11.5px;
    padding: 2px 8px;
    border-radius: var(--radius-sm);
    background: var(--bg-surface);
    border: 1px solid var(--border);
    color: var(--fg-primary);
    transition: all var(--transition-fast, 150ms ease);
  }
  :global(.cm-schema-enum-pill:hover) {
    border-color: var(--accent);
    color: var(--accent);
  }
  :global(.cm-schema-enum-pill.is-default) {
    border-color: color-mix(in srgb, var(--accent) 40%, transparent);
    background: color-mix(in srgb, var(--accent) 8%, var(--bg-surface));
  }
  :global(.cm-schema-enum-default-tag) {
    font-size: 9px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    padding: 0 4px;
    border-radius: 3px;
    background: color-mix(in srgb, var(--accent) 25%, transparent);
    color: var(--accent);
  }

  /* 4. Footer Constraints */
  :global(.cm-schema-tooltip-footer) {
    padding: var(--spacing-2) var(--spacing-4);
    background: var(--bg-surface);
    border-top: 1px solid var(--border);
    display: flex;
    align-items: center;
    gap: var(--spacing-3);
    flex-wrap: wrap;
    font-size: var(--font-size-xs, 12px);
    flex-shrink: 0;
  }
  :global(.cm-schema-constraint) {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  :global(.cm-constraint-label) {
    color: var(--fg-dim);
  }
  :global(.cm-schema-constraint code) {
    color: var(--fg-primary);
    font-family: var(--font-family-mono);
    font-weight: 600;
  }

  /* Fallback library styles for any third-party hover */
  :global(.cm6-json-schema-hover) {
    font-family: var(--font-family-sans);
    color: var(--fg-secondary);
  }
  :global(.cm6-json-schema-hover--description) {
    padding: var(--spacing-3) var(--spacing-4);
    font-size: var(--font-size-sm);
    line-height: 1.5;
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
