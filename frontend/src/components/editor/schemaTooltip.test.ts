import { describe, it, expect } from 'vitest';
import {
  parseJsonPointer,
  unescapeJsonPointer,
  formatSchemaType,
  formatDefaultValue,
  escapeHtml,
  buildSchemaTooltipHtml,
  createSchemaHoverOptions
} from './schemaTooltip';

describe('schemaTooltip', () => {
  describe('unescapeJsonPointer', () => {
    it('unescapes ~1 to / and ~0 to ~', () => {
      expect(unescapeJsonPointer('foo~1bar')).toBe('foo/bar');
      expect(unescapeJsonPointer('foo~0bar')).toBe('foo~bar');
      expect(unescapeJsonPointer('normal')).toBe('normal');
    });
  });

  describe('escapeHtml', () => {
    it('escapes HTML special characters', () => {
      expect(escapeHtml('<script>"test" & \'foo\'</script>')).toBe(
        '&lt;script&gt;&quot;test&quot; &amp; &#39;foo&#39;&lt;/script&gt;'
      );
    });
  });

  describe('parseJsonPointer', () => {
    it('handles empty or root pointer', () => {
      expect(parseJsonPointer('')).toEqual({
        propertyName: '',
        parentPath: '',
        fullPath: ''
      });
      expect(parseJsonPointer('/')).toEqual({
        propertyName: '',
        parentPath: '',
        fullPath: ''
      });
    });

    it('parses top-level properties', () => {
      expect(parseJsonPointer('/mode')).toEqual({
        propertyName: 'mode',
        parentPath: '',
        fullPath: 'mode'
      });
      expect(parseJsonPointer('/log-level')).toEqual({
        propertyName: 'log-level',
        parentPath: '',
        fullPath: 'log-level'
      });
    });

    it('parses nested object properties', () => {
      expect(parseJsonPointer('/routing/domainStrategy')).toEqual({
        propertyName: 'domainStrategy',
        parentPath: 'routing',
        fullPath: 'routing › domainStrategy'
      });
      expect(parseJsonPointer('/dns/fake-ip-filter')).toEqual({
        propertyName: 'fake-ip-filter',
        parentPath: 'dns',
        fullPath: 'dns › fake-ip-filter'
      });
    });

    it('formats array indexes into parent path brackets', () => {
      expect(parseJsonPointer('/proxies/0/cipher')).toEqual({
        propertyName: 'cipher',
        parentPath: 'proxies[0]',
        fullPath: 'proxies[0] › cipher'
      });
      expect(parseJsonPointer('/routing/rules/5/domain')).toEqual({
        propertyName: 'domain',
        parentPath: 'routing › rules[5]',
        fullPath: 'routing › rules[5] › domain'
      });
    });
  });

  describe('formatSchemaType', () => {
    it('formats primitive types', () => {
      expect(formatSchemaType({ type: 'string' })).toBe('string');
      expect(formatSchemaType({ type: 'integer' })).toBe('integer');
      expect(formatSchemaType({ type: 'boolean' })).toBe('boolean');
    });

    it('formats array of types', () => {
      expect(formatSchemaType({ type: ['string', 'number'] })).toBe('string | number');
    });

    it('formats oneOf / anyOf', () => {
      expect(
        formatSchemaType({
          oneOf: [{ type: 'integer' }, { type: 'string' }]
        })
      ).toBe('integer | string');

      expect(
        formatSchemaType({
          anyOf: [{ type: 'boolean' }, { type: 'string' }]
        })
      ).toBe('boolean | string');
    });

    it('returns empty string when no type info', () => {
      expect(formatSchemaType({})).toBe('');
      expect(formatSchemaType(null)).toBe('');
    });
  });

  describe('formatDefaultValue', () => {
    it('formats strings, booleans, and numbers', () => {
      expect(formatDefaultValue('rule')).toBe('"rule"');
      expect(formatDefaultValue(true)).toBe('true');
      expect(formatDefaultValue(7890)).toBe('7890');
    });

    it('returns empty string for undefined', () => {
      expect(formatDefaultValue(undefined)).toBe('');
    });
  });

  describe('buildSchemaTooltipHtml', () => {
    it('generates HTML with header, badges, markdown description, and enum pills', () => {
      const html = buildSchemaTooltipHtml({
        pointer: '/routing/domainStrategy',
        schema: {
          type: 'string',
          default: 'IPIfNonMatch',
          enum: ['AsIs', 'IPIfNonMatch', 'IPOnDemand'],
          description:
            'Стратегия разрешения доменов:\n- **AsIs** — без DNS.\n- **IPIfNonMatch** — при несовпадении.\n\n> Важное примечание.'
        },
        lang: 'ru'
      });

      // Check header
      expect(html).toContain('cm-schema-tooltip-header');
      expect(html).toContain('routing › ');
      expect(html).toContain('domainStrategy');
      expect(html).toContain('cm-schema-badge-type');
      expect(html).toContain('string');

      // Check default value badge
      expect(html).toContain('cm-schema-badge-default');
      expect(html).toContain('&quot;IPIfNonMatch&quot;');

      // Check description markdown
      expect(html).toContain('cm-schema-tooltip-description');
      expect(html).toContain('<strong>AsIs</strong>');
      expect(html).toContain('<blockquote>');

      // Check enums
      expect(html).toContain('cm-schema-tooltip-enums');
      expect(html).toContain('Допустимые значения:');
      expect(html).toContain('cm-schema-enum-pill');
      expect(html).toContain('is-default');
      expect(html).toContain('default</span>');
      expect(html).toContain('AsIs');
      expect(html).toContain('IPOnDemand');
    });

    it('renders constraints like minimum, maximum, format, pattern', () => {
      const html = buildSchemaTooltipHtml({
        pointer: '/port',
        schema: {
          type: 'integer',
          default: 7890,
          minimum: 1,
          maximum: 65535,
          format: 'port',
          description: 'Порт входящих подключений.'
        },
        lang: 'ru'
      });

      expect(html).toContain('cm-schema-tooltip-footer');
      expect(html).toContain('1 — 65535');
      expect(html).toContain('port');
    });

    it('renders English labels when lang is en', () => {
      const html = buildSchemaTooltipHtml({
        pointer: '/mode',
        schema: {
          type: 'string',
          default: 'rule',
          enum: ['rule', 'global'],
          description: 'Operating mode'
        },
        lang: 'en',
        isRequired: true
      });

      expect(html).toContain('Allowed values:');
      expect(html).toContain('required');
      expect(html).toContain('Default value');
    });
  });

  describe('createSchemaHoverOptions', () => {
    it('returns valid hover options with getHoverTexts', () => {
      const opts = createSchemaHoverOptions('ru');
      expect(typeof opts.getHoverTexts).toBe('function');
      expect(typeof opts.formatHover).toBe('function');

      const data = opts.getHoverTexts!({
        schema: { type: 'boolean', description: 'Enable feature' },
        pointer: '/tun/enable'
      }) as any;

      expect(data.pointer).toBe('/tun/enable');
      expect(data.schema.type).toBe('boolean');
    });
  });
});
