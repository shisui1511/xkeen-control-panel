import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { render } from 'svelte/server';
import Button from './Button.svelte';

const SOURCE = fs.readFileSync(path.join(__dirname, 'Button.svelte'), 'utf8');

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
});
