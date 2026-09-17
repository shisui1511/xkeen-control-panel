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

  it('does NOT leak className onto the wrapper when wrapperClass is not provided (120-REVIEW CR-05)', () => {
    // Regression guard: class= is styled box-model CSS in real usage
    // (.input, :global(.form-select) — both border/background/padding/
    // border-radius). If the wrapper <span> also received this class, it
    // would visually double-stack the box-model on top of the already
    // styled <select>, producing a nested "box in a box". The wrapper must
    // only ever carry 'xcp-select' plus an explicit wrapperClass opt-in.
    const { body } = render(Select, {
      props: {
        class: 'input'
      }
    });

    // Svelte SSR appends its own scope class (e.g. svelte-xxxxx) to both
    // elements, so we assert on membership rather than exact array equality.
    const wrapperMatch = body.match(/<span class="([^"]*)"/);
    expect(wrapperMatch).not.toBeNull();
    const wrapperClasses = (wrapperMatch?.[1] ?? '').split(/\s+/);
    expect(wrapperClasses).toContain('xcp-select');
    expect(wrapperClasses).not.toContain('input');

    expect(body).toMatch(/<select[^>]*class="input\b/);
  });

  it('applies wrapperClass on the wrapper only as an explicit opt-in, independent of class=', () => {
    const { body } = render(Select, {
      props: {
        class: 'input',
        wrapperClass: 'form-row-select'
      }
    });

    const wrapperMatch = body.match(/<span class="([^"]*)"/);
    expect(wrapperMatch).not.toBeNull();
    const wrapperClasses = (wrapperMatch?.[1] ?? '').split(/\s+/);
    expect(wrapperClasses).toContain('xcp-select');
    expect(wrapperClasses).toContain('form-row-select');
    expect(wrapperClasses).not.toContain('input');

    expect(body).toMatch(/<select[^>]*class="input\b/);
  });

  it('uses var(--font-size-sm) instead of hardcoded font-size', () => {
    expect(SOURCE).toMatch(/font-size:\s*var\(--font-size-sm\);/);
    expect(SOURCE).not.toMatch(/font-size:\s*13px;/);
  });
});
