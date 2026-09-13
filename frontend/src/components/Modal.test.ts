import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';
import Modal from './Modal.svelte';
import SOURCE from './Modal.svelte?raw';

describe('Modal accessibility & focus management', () => {
  it('renders nothing when isOpen is false', () => {
    const { body } = render(Modal, {
      props: {
        isOpen: false,
        title: 'Тестовое окно',
        onclose: () => {}
      }
    });
    expect(body).not.toContain('modal-backdrop');
    expect(body.replace(/<!--.*?-->/g, '').trim()).toBe('');
  });

  it('renders role="dialog" and aria-modal="true" when isOpen is true', () => {
    const { body } = render(Modal, {
      props: {
        isOpen: true,
        title: 'Тестовое окно',
        onclose: () => {}
      }
    });
    expect(body).toContain('role="dialog"');
    expect(body).toContain('aria-modal="true"');
    expect(body).toContain('aria-labelledby="modal-title"');
    expect(body).toContain('id="modal-title"');
  });

  it('uses ariaLabel prop when provided instead of aria-labelledby', () => {
    const { body } = render(Modal, {
      props: {
        isOpen: true,
        title: '',
        ariaLabel: 'Кастомный заголовок',
        onclose: () => {}
      }
    });
    expect(body).toContain('aria-label="Кастомный заголовок"');
    expect(body).not.toContain('aria-labelledby="modal-title"');
  });

  it('renders close button with aria-label', () => {
    const { body } = render(Modal, {
      props: {
        isOpen: true,
        title: 'Окно с кнопкой закрытия',
        onclose: () => {}
      }
    });
    expect(body).toContain('class="modal-close-btn');
    expect(body).toMatch(/aria-label="[^"]+"/);
  });

  it('contains keyboard trap and escape handler logic in source', () => {
    expect(SOURCE).toContain("event.key === 'Escape'");
    expect(SOURCE).toContain("event.key === 'Tab'");
    expect(SOURCE).toContain('previouslyFocusedElement');
    expect(SOURCE).toContain('getFocusableElements()');
  });
});
