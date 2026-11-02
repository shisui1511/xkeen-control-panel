import { prefersReducedMotion } from 'svelte/motion';
import type { Attachment } from 'svelte/attachments';
import {
  fade as svelteFade,
  fly as svelteFly,
  scale as svelteScale,
  slide as svelteSlide,
  type FadeParams,
  type FlyParams,
  type ScaleParams,
  type SlideParams,
  type TransitionConfig
} from 'svelte/transition';
import { flip as svelteFlip, type AnimationConfig, type FlipParams } from 'svelte/animate';
import { cubicOut } from 'svelte/easing';

// Движение в панели. Анимации гасятся по двум причинам: системная настройка
// prefers-reduced-motion и переключатель «Анимации» в настройках (localStorage
// 'animations' = 'false'). CSS-анимации гасит атрибут data-motion="reduced" на <html>
// (правила в styles/global.css; первый кадр ставит public/theme-init.js). Переходы Svelte
// идут через Web Animations API и CSS-правила не видят, поэтому их обёртки ниже сами
// обнуляют длительность.

const STORAGE_KEY = 'animations';

function readDisabled(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === 'false';
  } catch {
    return false;
  }
}

let disabledBySetting = $state(readDisabled());

export const motion = {
  /** true — анимации выключены настройкой или системой. Реактивно. */
  get reduced(): boolean {
    return disabledBySetting || prefersReducedMotion.current;
  },
  get enabledBySetting(): boolean {
    return !disabledBySetting;
  }
};

function applyAttribute() {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  if (disabledBySetting) root.setAttribute('data-motion', 'reduced');
  else root.removeAttribute('data-motion');
}

export function initMotion() {
  disabledBySetting = readDisabled();
  applyAttribute();
}

export function setAnimationsEnabled(enabled: boolean) {
  disabledBySetting = !enabled;
  try {
    localStorage.setItem(STORAGE_KEY, String(enabled));
  } catch {
    // localStorage недоступен — настройка действует до перезагрузки
  }
  applyAttribute();
}

/** Общий easing появления: быстрый старт, мягкая посадка. */
export const easeOut = cubicOut;

function still<T extends object>(params: T): T {
  return motion.reduced ? { ...params, duration: 0, delay: 0 } : params;
}

export function fade(node: Element, params: FadeParams = {}): TransitionConfig {
  return svelteFade(node, still(params));
}

export function fly(node: Element, params: FlyParams = {}): TransitionConfig {
  return svelteFly(node, still(params));
}

export function slide(node: Element, params: SlideParams = {}): TransitionConfig {
  return svelteSlide(node, still(params));
}

export function scale(node: Element, params: ScaleParams = {}): TransitionConfig {
  return svelteScale(node, still(params));
}

export function flip(
  node: Element,
  fromTo: { from: DOMRect; to: DOMRect },
  params: FlipParams = {}
): AnimationConfig {
  return svelteFlip(node, fromTo, still(params));
}

/**
 * Появление страницы: короткий подъём на несколько пикселей с проявлением.
 * Только на вход: уходящая страница исчезает сразу, иначе две страницы на
 * мгновение стоят друг под другом и макет прыгает.
 */
export function pageIn(
  node: Element,
  { duration = 220, y = 6 }: { duration?: number; y?: number } = {}
): TransitionConfig {
  if (motion.reduced) return { duration: 0 };
  return {
    duration,
    easing: cubicOut,
    css: (tt, u) => `opacity: ${tt}; transform: translateY(${u * y}px);`
  };
}

/**
 * Скользящий индикатор активного элемента (подложка сегмента, черта вкладки).
 * Пишет на контейнер --ind-x/--ind-y/--ind-w/--ind-h и data-indicator:
 * "ready" — позиция известна, "animate" — позицию можно менять с переходом
 * (ставится кадром позже, чтобы индикатор не выезжал из нуля при монтировании).
 * Контейнер должен быть позиционирован (position: relative), индикатор рисует CSS.
 */
export function slidingIndicator(activeSelector: string): Attachment<HTMLElement> {
  return (el) => {
    let raf = 0;
    let armRaf = 0;
    let firstPaint = true;

    const measure = () => {
      const active = el.querySelector<HTMLElement>(activeSelector);
      if (!active || active.offsetWidth === 0) {
        el.removeAttribute('data-indicator');
        firstPaint = true;
        return;
      }
      el.style.setProperty('--ind-x', `${active.offsetLeft}px`);
      el.style.setProperty('--ind-y', `${active.offsetTop}px`);
      el.style.setProperty('--ind-w', `${active.offsetWidth}px`);
      el.style.setProperty('--ind-h', `${active.offsetHeight}px`);
      if (firstPaint) {
        firstPaint = false;
        el.setAttribute('data-indicator', 'ready');
        cancelAnimationFrame(armRaf);
        armRaf = requestAnimationFrame(() => {
          armRaf = requestAnimationFrame(() => {
            if (el.hasAttribute('data-indicator')) el.setAttribute('data-indicator', 'animate');
          });
        });
      }
    };

    const scheduled = () => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(measure);
    };

    const resizeObserver =
      typeof ResizeObserver !== 'undefined' ? new ResizeObserver(scheduled) : null;
    const observeChildren = () => {
      if (!resizeObserver) return;
      resizeObserver.disconnect();
      resizeObserver.observe(el);
      for (const child of Array.from(el.children)) resizeObserver.observe(child);
    };

    // Смена класса активного элемента приходит microtask'ом до отрисовки:
    // индикатор переезжает в том же кадре, без мигания пустого состояния.
    const mutationObserver = new MutationObserver((records) => {
      if (records.some((r) => r.type === 'childList')) observeChildren();
      measure();
    });
    mutationObserver.observe(el, {
      subtree: true,
      childList: true,
      attributes: true,
      attributeFilter: ['class', 'aria-selected', 'aria-pressed', 'disabled']
    });

    measure();
    observeChildren();
    // Шрифты догружаются после первого замера и меняют ширину подписей
    document.fonts?.ready.then(scheduled).catch(() => {});

    return () => {
      cancelAnimationFrame(raf);
      cancelAnimationFrame(armRaf);
      mutationObserver.disconnect();
      resizeObserver?.disconnect();
      el.removeAttribute('data-indicator');
    };
  };
}
