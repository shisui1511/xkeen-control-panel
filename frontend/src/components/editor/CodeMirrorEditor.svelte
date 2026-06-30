<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
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
    bracketMatching,
    foldKeymap
  } from '@codemirror/language';
  import { lintKeymap, linter } from '@codemirror/lint';
  import { json, jsonParseLinter, jsonLanguage } from '@codemirror/lang-json';
  import { yaml, yamlLanguage } from '@codemirror/lang-yaml';
  import { hoverTooltip } from '@codemirror/view';

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
      doc: content,
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
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
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
    min-height: 500px;
    position: relative;
  }
  :global(.cm-editor) {
    height: 100%;
  }
</style>
