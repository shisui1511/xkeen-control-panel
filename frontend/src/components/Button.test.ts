import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';
import Button from './Button.svelte';
import SOURCE from './Button.svelte?raw';

describe('Button — предупреждающий вариант', () => {
  it('renders the btn-warning class for variant="warning"', () => {
    const { body } = render(Button, { props: { variant: 'warning' } });
    expect(body).toMatch(/class="btn btn-warning[^"]*"/);
  });

  it('declares a .btn-warning CSS rule in the component styles (not just the TS type)', () => {
    expect(SOURCE).toMatch(/\.btn-warning\s*{/);
  });

  it('declares a .btn-warning hover rule matching the danger variant convention', () => {
    expect(SOURCE).toMatch(/\.btn-warning:hover:not\(:disabled\)\s*{/);
  });

  it('renders aria-label when ariaLabel prop is provided', () => {
    const { body } = render(Button, { props: { ariaLabel: 'Тестовое действие' } });
    expect(body).toContain('aria-label="Тестовое действие"');
  });

  it('appends custom class and style to button', () => {
    const { body } = render(Button, { props: { class: 'custom-btn', style: 'width: 100%;' } });
    expect(body).toMatch(/class="btn btn-primary custom-btn[^"]*"/);
    expect(body).toContain('style="width: 100%;"');
  });
});
