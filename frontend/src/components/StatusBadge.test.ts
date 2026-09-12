import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';
import StatusBadge from './StatusBadge.svelte';

describe('StatusBadge', () => {
  it('renders status-badge and variant classes with the label for variant="running"', () => {
    const { body } = render(StatusBadge, { props: { variant: 'running', label: 'Работает' } });
    expect(body).toMatch(/class="status-badge running"/);
    expect(body).toContain('Работает');
  });

  it('applies idle and info modifiers without upper-casing the label', () => {
    const idle = render(StatusBadge, { props: { variant: 'idle', label: 'Ожидание' } }).body;
    expect(idle).toMatch(/class="status-badge idle"/);
    expect(idle).toContain('Ожидание');
    expect(idle).not.toContain('ОЖИДАНИЕ');

    const info = render(StatusBadge, { props: { variant: 'info', label: 'Информация' } }).body;
    expect(info).toMatch(/class="status-badge info"/);
  });

  it('renders a colour dot as a visual anchor before the label', () => {
    const { body } = render(StatusBadge, { props: { variant: 'stopped', label: 'Остановлено' } });
    expect(body).toContain('status-badge-dot');
  });
});
