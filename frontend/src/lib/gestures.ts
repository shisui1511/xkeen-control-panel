import type { Attachment } from 'svelte/attachments';

// Жесты для сенсорных экранов. Всё — через Pointer/Touch Events без библиотек;
// мышь и перо не участвуют (жесты только для пальца). Вертикальная прокрутка
// страницы не блокируется: жест захватывается только когда движение явно идёт
// в его сторону.

const reduceMotion = () =>
  typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

interface SwipeOptions {
  /** Направление закрытия: вниз (лист) или влево (меню, строка). */
  direction: 'down' | 'left';
  onDismiss: () => void;
  /** Элемент, который двигается вслед за пальцем; по умолчанию — сам узел. */
  target?: () => HTMLElement | null;
  /** Порог в пикселях; по умолчанию 80. */
  threshold?: number;
  /** Включено ли (например, только на телефоне). */
  enabled?: () => boolean;
  /** Для «вниз»: жест начинается только если содержимое прокручено к началу. */
  scroller?: () => HTMLElement | null;
}

/**
 * Свайп-закрытие: элемент едет за пальцем, после порога (или быстрого броска)
 * вызывается onDismiss, иначе возвращается на место.
 */
export function swipeToDismiss(opts: SwipeOptions): Attachment<HTMLElement> {
  return (node) => {
    const threshold = opts.threshold ?? 80;
    let startX = 0;
    let startY = 0;
    let startT = 0;
    let tracking = false;
    let engaged = false;
    let offset = 0;
    const moving = () => opts.target?.() ?? node;

    // Не дотянули до порога — элемент плавно возвращается на место
    function snapBack() {
      const el = moving();
      el.style.transition = reduceMotion() ? '' : 'transform 200ms cubic-bezier(0.16, 1, 0.3, 1)';
      el.style.transform = '';
      setTimeout(() => (el.style.transition = ''), 220);
    }

    function onStart(e: TouchEvent) {
      if (e.touches.length !== 1 || (opts.enabled && !opts.enabled())) return;
      const t = e.touches[0];
      startX = t.clientX;
      startY = t.clientY;
      startT = performance.now();
      tracking = true;
      engaged = false;
      offset = 0;
    }

    function onMove(e: TouchEvent) {
      if (!tracking) return;
      const t = e.touches[0];
      const dx = t.clientX - startX;
      const dy = t.clientY - startY;
      if (!engaged) {
        if (Math.abs(dx) < 8 && Math.abs(dy) < 8) return;
        const along = opts.direction === 'down' ? dy : -dx;
        const across = opts.direction === 'down' ? Math.abs(dx) : Math.abs(dy);
        const scroller = opts.scroller?.();
        if (along <= 0 || across > along || (scroller && scroller.scrollTop > 0)) {
          tracking = false;
          return;
        }
        engaged = true;
      }
      offset = Math.max(0, opts.direction === 'down' ? dy : -dx);
      if (e.cancelable) e.preventDefault();
      const el = moving();
      el.style.transition = 'none';
      el.style.transform =
        opts.direction === 'down' ? `translateY(${offset}px)` : `translateX(${-offset}px)`;
    }

    function onEnd() {
      if (!tracking) return;
      tracking = false;
      if (!engaged) return;
      const velocity = offset / Math.max(1, performance.now() - startT);
      if (offset >= threshold || velocity > 0.6) {
        const el = moving();
        el.style.transition = '';
        el.style.transform = '';
        opts.onDismiss();
      } else {
        snapBack();
      }
    }

    node.addEventListener('touchstart', onStart, { passive: true });
    node.addEventListener('touchmove', onMove, { passive: false });
    node.addEventListener('touchend', onEnd);
    node.addEventListener('touchcancel', onEnd);
    return () => {
      node.removeEventListener('touchstart', onStart);
      node.removeEventListener('touchmove', onMove);
      node.removeEventListener('touchend', onEnd);
      node.removeEventListener('touchcancel', onEnd);
    };
  };
}

/**
 * Обновление потягиванием вниз от начала страницы (как в мобильных
 * приложениях). Индикатор — круглая метка под шапкой, её поворот показывает,
 * сколько осталось до порога. Работает только с пальцем и только когда
 * окно прокручено к самому верху.
 */
export function pullToRefresh(
  onRefresh: () => Promise<unknown> | unknown
): Attachment<HTMLElement> {
  return (node) => {
    const THRESHOLD = 72;
    const MAX = 110;
    let startY = 0;
    let pulling = false;
    let dist = 0;
    let busy = false;

    const ind = document.createElement('div');
    ind.className = 'ptr-indicator';
    ind.setAttribute('aria-hidden', 'true');
    ind.innerHTML =
      '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5"/></svg>';
    document.body.appendChild(ind);

    function paint() {
      const p = Math.min(1, dist / THRESHOLD);
      ind.style.opacity = String(p);
      ind.style.transform = `translate(-50%, ${Math.min(dist, MAX) * 0.6}px) rotate(${p * 270}deg)`;
      ind.classList.toggle('is-armed', dist >= THRESHOLD);
    }

    function onStart(e: TouchEvent) {
      if (busy || e.touches.length !== 1 || window.scrollY > 0) return;
      if (!window.matchMedia('(max-width: 768px)').matches) return;
      if (
        (e.target as HTMLElement).closest('input, textarea, select, [contenteditable], .cm-editor')
      )
        return;
      startY = e.touches[0].clientY;
      pulling = true;
      dist = 0;
    }

    function onMove(e: TouchEvent) {
      if (!pulling) return;
      const dy = e.touches[0].clientY - startY;
      if (dy <= 0 || window.scrollY > 0) {
        pulling = dy > -4;
        dist = 0;
        paint();
        return;
      }
      // Сопротивление: чем дальше тянут, тем медленнее едет метка
      dist = dy * 0.55;
      if (e.cancelable) e.preventDefault();
      paint();
    }

    async function onEnd() {
      if (!pulling) return;
      pulling = false;
      if (dist >= THRESHOLD) {
        busy = true;
        ind.classList.add('is-spinning');
        ind.style.transform = `translate(-50%, ${THRESHOLD * 0.6}px)`;
        try {
          await onRefresh();
        } finally {
          busy = false;
          ind.classList.remove('is-spinning', 'is-armed');
          dist = 0;
          paint();
        }
      } else {
        dist = 0;
        paint();
      }
    }

    node.addEventListener('touchstart', onStart, { passive: true });
    node.addEventListener('touchmove', onMove, { passive: false });
    node.addEventListener('touchend', onEnd);
    node.addEventListener('touchcancel', onEnd);
    paint();
    return () => {
      node.removeEventListener('touchstart', onStart);
      node.removeEventListener('touchmove', onMove);
      node.removeEventListener('touchend', onEnd);
      node.removeEventListener('touchcancel', onEnd);
      ind.remove();
    };
  };
}
