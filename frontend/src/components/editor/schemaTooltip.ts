import MarkdownIt from 'markdown-it';
import type { Lang } from '../../i18n';
import type { EditorView } from '@codemirror/view';
import type { HoverOptions, FoundCursorData } from 'codemirror-json-schema';

const md = new MarkdownIt({
  html: false,
  linkify: true,
  typographer: true,
  breaks: true
});

export interface ParsedPointer {
  propertyName: string;
  parentPath: string;
  fullPath: string;
}

export function unescapeJsonPointer(token: string): string {
  return token.replace(/~1/g, '/').replace(/~0/g, '~');
}

export function escapeHtml(str: string): string {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/**
 * Parses a JSON pointer (e.g. "/routing/domainStrategy" or "/proxies/0/cipher")
 * into user-friendly breadcrumb segments and property name.
 */
export function parseJsonPointer(pointer: string): ParsedPointer {
  if (!pointer || pointer === '/') {
    return { propertyName: '', parentPath: '', fullPath: '' };
  }

  const rawSegments = pointer.replace(/^\/+/, '').split('/').map(unescapeJsonPointer);

  if (rawSegments.length === 0) {
    return { propertyName: '', parentPath: '', fullPath: '' };
  }

  // Format array indexes cleanly into parent paths:
  // e.g. ["proxies", "0", "cipher"] -> parent: "proxies[0]", name: "cipher"
  const formatted: string[] = [];
  for (const seg of rawSegments) {
    if (/^\d+$/.test(seg) && formatted.length > 0) {
      formatted[formatted.length - 1] += `[${seg}]`;
    } else {
      formatted.push(seg);
    }
  }

  const propertyName = formatted.pop() || '';
  const parentPath = formatted.join(' › ');
  const fullPath = parentPath ? `${parentPath} › ${propertyName}` : propertyName;

  return { propertyName, parentPath, fullPath };
}

/**
 * Extracts a clean type representation from a JSON Schema node.
 */
export function formatSchemaType(schema: any): string {
  if (!schema) return '';

  if (schema.type) {
    if (Array.isArray(schema.type)) {
      return schema.type.join(' | ');
    }
    return String(schema.type);
  }

  if (Array.isArray(schema.oneOf)) {
    const types = schema.oneOf.map((item: any) => formatSchemaType(item)).filter(Boolean);
    const unique = Array.from(new Set(types));
    return unique.length > 0 ? unique.join(' | ') : 'oneOf';
  }

  if (Array.isArray(schema.anyOf)) {
    const types = schema.anyOf.map((item: any) => formatSchemaType(item)).filter(Boolean);
    const unique = Array.from(new Set(types));
    return unique.length > 0 ? unique.join(' | ') : 'anyOf';
  }

  if (schema.allOf) {
    return 'allOf';
  }

  return '';
}

/**
 * Formats a default value into a compact readable string.
 */
export function formatDefaultValue(val: unknown): string {
  if (val === undefined) return '';
  if (typeof val === 'string') return JSON.stringify(val);
  if (typeof val === 'number' || typeof val === 'boolean') return String(val);
  try {
    return JSON.stringify(val);
  } catch {
    return String(val);
  }
}

export interface SchemaTooltipData {
  message?: string;
  typeInfo?: string;
  pointer: string;
  schema: any;
  lang: Lang;
  isRequired?: boolean;
}

/**
 * Generates semantic HTML string for the schema hover tooltip.
 */
export function buildSchemaTooltipHtml(data: SchemaTooltipData): string {
  const { schema = {}, pointer = '', lang = 'ru', isRequired = false } = data;
  const isRu = lang === 'ru';
  const { propertyName, parentPath } = parseJsonPointer(pointer);
  const typeStr = formatSchemaType(schema);
  const hasDefault = schema.default !== undefined;
  const defaultStr = hasDefault ? formatDefaultValue(schema.default) : '';

  let html = '';

  // 1. Header
  const pathPart = parentPath
    ? `<span class="cm-schema-tooltip-path">${escapeHtml(parentPath)} › </span>`
    : '';
  const namePart = `<span class="cm-schema-tooltip-name">${escapeHtml(propertyName || schema.title || 'Schema')}</span>`;

  let badgesPart = '';
  if (typeStr) {
    badgesPart += `<span class="cm-schema-badge-type">${escapeHtml(typeStr)}</span>`;
  }
  if (isRequired) {
    badgesPart += `<span class="cm-schema-badge-required">${isRu ? 'обязательно' : 'required'}</span>`;
  }
  if (hasDefault) {
    const defTitle = isRu ? 'Значение по умолчанию' : 'Default value';
    badgesPart += `<span class="cm-schema-badge-default" title="${defTitle}"><span class="cm-schema-badge-label">def:</span><code>${escapeHtml(defaultStr)}</code></span>`;
  }

  html += `
  <div class="cm-schema-tooltip-header">
    <div class="cm-schema-tooltip-title-row">
      <div class="cm-schema-tooltip-name-group">
        ${pathPart}${namePart}
      </div>
      <div class="cm-schema-tooltip-badges">
        ${badgesPart}
      </div>
    </div>
  </div>`;

  // 2. Body (Description in Markdown)
  if (schema.description) {
    html += `
  <div class="cm-schema-tooltip-body">
    <div class="cm-schema-tooltip-description">
      ${md.render(schema.description)}
    </div>
  </div>`;
  }

  // 3. Enum values
  if (Array.isArray(schema.enum) && schema.enum.length > 0) {
    const enumPills = schema.enum
      .map((val: any) => {
        const isValDefault = hasDefault && schema.default === val;
        const defaultTag = isValDefault
          ? '<span class="cm-schema-enum-default-tag">default</span>'
          : '';
        return `<span class="cm-schema-enum-pill${isValDefault ? ' is-default' : ''}"><code>${escapeHtml(String(val))}</code>${defaultTag}</span>`;
      })
      .join('');

    html += `
  <div class="cm-schema-tooltip-enums">
    <div class="cm-schema-tooltip-enums-header">
      <svg class="cm-schema-enums-icon" viewBox="0 0 16 16" width="12" height="12" fill="currentColor">
        <path d="M2 3.5a.5.5 0 0 1 .5-.5h11a.5.5 0 0 1 0 1h-11a.5.5 0 0 1-.5-.5zm0 4.5a.5.5 0 0 1 .5-.5h11a.5.5 0 0 1 0 1h-11a.5.5 0 0 1-.5-.5zm0 4.5a.5.5 0 0 1 .5-.5h7a.5.5 0 0 1 0 1h-7a.5.5 0 0 1-.5-.5z"/>
      </svg>
      <span class="cm-schema-enums-title">${isRu ? 'Допустимые значения:' : 'Allowed values:'}</span>
    </div>
    <div class="cm-schema-tooltip-enums-list">
      ${enumPills}
    </div>
  </div>`;
  }

  // 4. Footer constraints
  const hasMinMax = schema.minimum !== undefined || schema.maximum !== undefined;
  const hasFormat = Boolean(schema.format);
  const hasPattern = Boolean(schema.pattern);

  if (hasMinMax || hasFormat || hasPattern) {
    let constraintsHtml = '';
    if (hasMinMax) {
      constraintsHtml += `
      <span class="cm-schema-constraint">
        <span class="cm-constraint-label">${isRu ? 'Диапазон:' : 'Range:'}</span>
        <code>${escapeHtml(String(schema.minimum ?? '...'))} — ${escapeHtml(String(schema.maximum ?? '...'))}</code>
      </span>`;
    }
    if (hasFormat) {
      constraintsHtml += `
      <span class="cm-schema-constraint">
        <span class="cm-constraint-label">${isRu ? 'Формат:' : 'Format:'}</span>
        <code>${escapeHtml(String(schema.format))}</code>
      </span>`;
    }
    if (hasPattern) {
      constraintsHtml += `
      <span class="cm-schema-constraint">
        <span class="cm-constraint-label">${isRu ? 'Паттерн:' : 'Pattern:'}</span>
        <code>${escapeHtml(String(schema.pattern))}</code>
      </span>`;
    }

    html += `
  <div class="cm-schema-tooltip-footer">
    ${constraintsHtml}
  </div>`;
  }

  return html;
}

/**
 * Builds the complete DOM tree for the schema hover tooltip.
 */
export function renderSchemaTooltip(data: SchemaTooltipData): HTMLElement {
  const root = document.createElement('div');
  root.className = 'cm-schema-tooltip';
  root.innerHTML = buildSchemaTooltipHtml(data);
  return root;
}

/**
 * Creates options for codemirror-json-schema's jsonSchemaHover / yamlSchemaHover.
 */
export function createSchemaHoverOptions(lang: Lang = 'ru'): HoverOptions {
  return {
    getHoverTexts(data: FoundCursorData): any {
      const { schema = {}, pointer = '' } = data;

      const tooltipData: SchemaTooltipData = {
        message: schema.description || '',
        typeInfo: formatSchemaType(schema),
        pointer,
        schema,
        lang
      };

      return tooltipData;
    },
    formatHover(data: any): HTMLElement {
      return renderSchemaTooltip(data);
    }
  };
}

/**
 * Enhances a CodeMirror Tooltip with smart vertical positioning
 * so tooltips near the top of the editor open downward instead of clipping offscreen.
 */
export function createEnhancedTooltip(tooltip: any, view: EditorView, pos: number) {
  if (!tooltip) return null;

  try {
    const coords = view.coordsAtPos(pos);
    if (coords) {
      const editorRect = view.dom.getBoundingClientRect();
      const distFromTop = coords.top - editorRect.top;
      // If cursor is within top 200px of editor, open downward
      if (distFromTop < 200) {
        return {
          ...tooltip,
          above: false
        };
      }
    }
  } catch {
    // Keep default
  }

  return tooltip;
}
