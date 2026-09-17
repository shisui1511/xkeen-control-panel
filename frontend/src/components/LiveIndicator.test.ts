import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';
import LiveIndicator from './LiveIndicator.svelte';

describe('LiveIndicator', () => {
  it('applies the connected modifier when live is true', () => {
    const { body } = render(LiveIndicator, { props: { live: true, label: 'Подключено' } });
    expect(body).toMatch(/class="status-indicator connected"/);
    expect(body).toContain('Подключено');
  });

  it('does not apply the connected modifier when live is false', () => {
    const { body } = render(LiveIndicator, { props: { live: false, label: 'Отключено' } });
    expect(body).toMatch(/class="status-indicator"/);
    expect(body).not.toContain('connected');
  });
});
