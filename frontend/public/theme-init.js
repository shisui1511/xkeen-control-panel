// Блокирующий скрипт первого кадра: ставит data-theme и data-accent до отрисовки.
// Внешний файл, потому что CSP запрещает inline-скрипты (script-src 'self').
// Логика совпадает с initTheme()/initAccent() в src/main.ts; источник правды
// при монтировании приложения — main.ts. Плотность (theme_density) не трогается.
(function () {
  var theme = '';
  var accent = '';
  try {
    theme = localStorage.getItem('theme') || '';
    accent = localStorage.getItem('accent') || '';
  } catch (e) {
    // localStorage недоступен (приватный режим, заблокированные cookies) — системная тема
  }
  var root = document.documentElement;
  if (theme !== 'light' && theme !== 'dark') {
    var prefersDark =
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-color-scheme: dark)').matches;
    theme = prefersDark ? 'dark' : 'light';
  }
  root.setAttribute('data-theme', theme);
  if (accent === 'indigo' || accent === 'steel' || accent === 'graphite') {
    root.setAttribute('data-accent', accent);
  }
})();
