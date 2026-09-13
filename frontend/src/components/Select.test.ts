import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';
import Select from './Select.svelte';
import SOURCE from './Select.svelte?raw';

describe('Select', () => {
  it('renders select with options and wrapper classes', () => {
    const options = [
      { value: 'opt1', label: 'Option 1' },
      { value: 'opt2', label: 'Option 2' }
    ];
    const { body } = render(Select, {
      props: {
        options,
        value: 'opt1',
        class: 'custom-select',
        wrapperClass: 'custom-wrapper'
      }
    });

    expect(body).toMatch(/class="xcp-select custom-wrapper\b/);
    expect(body).toMatch(/class="custom-select\b/);
    expect(body).toContain('value="opt1"');
    expect(body).toContain('Option 1');
    expect(body).toContain('Option 2');
  });

  it('falls back to className on wrapper when wrapperClass is not provided', () => {
    const { body } = render(Select, {
      props: {
        class: 'group-select'
      }
    });

    expect(body).toMatch(/class="xcp-select group-select\b/);
    expect(body).toMatch(/<select[^>]*class="group-select\b/);
  });

  it('uses var(--font-size-sm) instead of hardcoded font-size', () => {
    expect(SOURCE).toMatch(/font-size:\s*var\(--font-size-sm\);/);
    expect(SOURCE).not.toMatch(/font-size:\s*13px;/);
  });
});
