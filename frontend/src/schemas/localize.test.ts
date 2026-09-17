import { describe, it, expect } from 'vitest';
import { localizeSchema } from './localize';

describe('localizeSchema', () => {
  it('resolves a { ru, en } description to the requested language', () => {
    const schema = { description: { ru: 'Русский текст', en: 'English text' } };
    expect((localizeSchema(schema, 'ru') as any).description).toBe('Русский текст');
    expect((localizeSchema(schema, 'en') as any).description).toBe('English text');
  });

  it('falls back to en, then ru, when the requested language is missing', () => {
    const enOnly = { description: { en: 'English only' } };
    expect((localizeSchema(enOnly, 'ru') as any).description).toBe('English only');

    const ruOnly = { description: { ru: 'Только русский' } };
    expect((localizeSchema(ruOnly, 'en') as any).description).toBe('Только русский');
  });

  it('leaves a plain string description untouched', () => {
    const schema = { description: 'plain string' };
    expect((localizeSchema(schema, 'ru') as any).description).toBe('plain string');
  });

  it('recurses into nested properties, arrays and items', () => {
    const schema = {
      type: 'object',
      properties: {
        foo: { description: { ru: 'Фу', en: 'Foo' } },
        list: {
          type: 'array',
          items: { description: { ru: 'Элемент', en: 'Item' } }
        }
      },
      allOf: [{ description: { ru: 'Ветка', en: 'Branch' } }]
    };
    const localized = localizeSchema(schema, 'ru') as any;
    expect(localized.properties.foo.description).toBe('Фу');
    expect(localized.properties.list.items.description).toBe('Элемент');
    expect(localized.allOf[0].description).toBe('Ветка');
  });

  it('does not mutate the source schema', () => {
    const schema = { description: { ru: 'Р', en: 'E' } };
    localizeSchema(schema, 'ru');
    expect(typeof schema.description).toBe('object');
  });
});
