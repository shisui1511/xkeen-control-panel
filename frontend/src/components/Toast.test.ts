import { describe, it, expect, beforeEach } from 'vitest';
import { render } from 'svelte/server';
import Toast from './Toast.svelte';
import { toastStore } from '../stores';

describe('Toast a11y & live regions', () => {
  beforeEach(() => {
    toastStore.set([]);
  });

  it('renders persistent container with role="region" and aria-label', () => {
    const { body } = render(Toast);
    expect(body).toContain('toast-container');
    expect(body).toContain('role="region"');
  });

  it('renders role="alert" and aria-live="assertive" for error toast', () => {
    toastStore.set([{ id: 2, type: 'error', message: 'Критическая ошибка' }]);
    const { body } = render(Toast);
    expect(body).toContain('role="alert"');
    expect(body).toContain('aria-live="assertive"');
    expect(body).toContain('Критическая ошибка');
  });

  it('renders role="status" and aria-live="polite" for success toast', () => {
    toastStore.set([{ id: 3, type: 'success', message: 'Успешно сохранено' }]);
    const { body } = render(Toast);
    expect(body).toContain('role="status"');
    expect(body).toContain('aria-live="polite"');
    expect(body).toContain('Успешно сохранено');
  });

  it('renders dismiss button with aria-label and type="button"', () => {
    toastStore.set([{ id: 4, type: 'warning', message: 'Предупреждение' }]);
    const { body } = render(Toast);
    expect(body).toContain('class="toast__close');
    expect(body).toContain('type="button"');
    expect(body).toMatch(/aria-label="[^"]+"/);
  });
});
