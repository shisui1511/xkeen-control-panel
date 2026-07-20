<script lang="ts">
  import { onMount, onDestroy, untrack } from 'svelte';
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

  function getSchemaExtensions(filePath: string, expert: boolean = false) {
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

    // Track path, expertMode, schemaEnabled reactively
    const currentPath = path;
    const currentExpertMode = expertMode;
    const currentSchemaEnabled = schemaEnabled;

    const lang = currentPath.endsWith('.yaml') || currentPath.endsWith('.yml') ? yaml() : json();
    const schemaExts = getSchemaExtensions(currentPath, currentExpertMode);

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

  onDestroy(() => {
    if (view) {
      view.destroy();
      view = null;
    }
  });
</script>

<div class="editor-cm-wrapper" bind:this={editorContainer}></div>

<style>
  .editor-cm-wrapper {
    height: 100%;
    min-height: 0;
    position: relative;
  }
  :global(.cm-editor) {
    height: 100% !important;
    font-size: 14px;
    background: var(--cm-bg) !important;
    color: var(--fg-primary) !important;
  }
  :global(.cm-scroller) {
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

  @media (max-width: 768px) {
    :global(.cm-editor) {
      font-size: 16px;
    }
    :global(.cm-gutters) {
      display: none !important;
    }
  }
</style>
