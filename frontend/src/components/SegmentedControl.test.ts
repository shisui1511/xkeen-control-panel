import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';
import SegmentedControl, { resolveSegmentValue } from './SegmentedControl.svelte';

describe('SegmentedControl', () => {
  it('renders one button per item with aria-pressed true on the active one', () => {
    const items = [
      { value: 'grid', label: 'Grid' },
      { value: 'list', label: 'List' }
    ];
    const { body } = render(SegmentedControl, { props: { items, value: 'list' } });
    const buttonCount = (body.match(/class="seg-item/g) || []).length;
    expect(buttonCount).toBe(2);
    expect(body).toMatch(/data-value="list" aria-pressed="true"/);
    expect(body).toMatch(/data-value="grid" aria-pressed="false"/);
  });

  it('resolves a click on a segment to that segment value (change signal for onchange)', () => {
    const item = { value: 'list', label: 'List' };
    expect(resolveSegmentValue(item)).toBe('list');
  });

  it('keeps two same-label segments independent — identity is by value, not label', () => {
    const items = [
      { value: 'a', label: 'Same' },
      { value: 'b', label: 'Same' }
    ];
    expect(resolveSegmentValue(items[0])).toBe('a');
    expect(resolveSegmentValue(items[1])).toBe('b');

    const { body } = render(SegmentedControl, { props: { items, value: 'a' } });
    expect(body).toMatch(/data-value="a" aria-pressed="true"/);
    expect(body).toMatch(/data-value="b" aria-pressed="false"/);
  });
});
