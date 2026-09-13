import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';
import Tabs, { resolveTabValue } from './Tabs.svelte';

describe('Tabs', () => {
  it('renders one button per item with the active one matching value', () => {
    const items = [
      { value: 'a', label: 'A' },
      { value: 'b', label: 'B' },
      { value: 'c', label: 'C' }
    ];
    const { body } = render(Tabs, { props: { items, value: 'b' } });
    const buttonCount = (body.match(/class="tab-btn/g) || []).length;
    expect(buttonCount).toBe(3);
    expect(body).toMatch(/class="tab-btn[^"]*\bactive\b[^"]*"[^>]*>\s*B\s*<\/button>/);
    expect(body).toContain('role="tab"');
    expect(body).toContain('aria-selected="true"');
    expect(body).toContain('aria-selected="false"');
  });

  it('does not render a container for an empty items array', () => {
    const { body } = render(Tabs, { props: { items: [], value: '' } });
    expect(body).not.toContain('class="tabs"');
  });

  it('resolves a click on an inactive tab to that tab value (change signal for onchange)', () => {
    const inactive = { value: 'b', label: 'B' };
    expect(resolveTabValue(inactive, 'a')).toBe('b');
  });

  it('keeps the current value when the clicked tab is disabled', () => {
    const disabled = { value: 'b', label: 'B', disabled: true };
    expect(resolveTabValue(disabled, 'a')).toBe('a');
  });

  it('preserves item order and treats value, not label, as identity', () => {
    const items = [
      { value: 'x', label: 'Same' },
      { value: 'y', label: 'Same' }
    ];
    const { body } = render(Tabs, { props: { items, value: 'y' } });
    const order = [...body.matchAll(/class="tab-btn[^"]*"[^>]*>\s*(Same)\s*</g)];
    expect(order.length).toBe(2);
    expect(body).toMatch(/class="tab-btn[^"]*\bactive\b[^"]*"[^>]*>\s*Same\s*<\/button>/);
  });

  it('renders tabs-pill class when variant="pill"', () => {
    const items = [
      { value: 'x', label: 'X' },
      { value: 'y', label: 'Y' }
    ];
    const { body } = render(Tabs, { props: { items, value: 'x', variant: 'pill' } });
    expect(body).toContain('tabs-pill');
  });
});
