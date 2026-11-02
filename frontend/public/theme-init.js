// Блокирующий скрипт первого кадра: ставит data-theme, data-scheme и data-accent до отрисовки.
// Внешний файл, потому что CSP запрещает inline-скрипты (script-src 'self').
// Список тем совпадает с THEMES в src/lib/theme.ts; источник правды
// при монтировании приложения — initTheme()/initAccent() в src/main.ts.
// Плотность (theme_density) не трогается.
(function () {
  // id -> [схема, цвет строки браузера]
  var THEMES = {
    dark: ['dark', '#07182a'],
    charcoal: ['dark', '#000000'],
    dusk: ['dark', '#171b21'],
    amber: ['dark', '#0a0704'],
    light: ['light', '#22303d'] // «Туман»
  };
  var theme = '';
  var accent = '';
  try {
    theme = localStorage.getItem('theme') || '';
    accent = localStorage.getItem('accent') || '';
  } catch (e) {
    // localStorage недоступен (приватный режим, заблокированные cookies) — системная тема
  }
  if (theme === 'fog') theme = 'light'; // устаревший id «Тумана»
  var root = document.documentElement;
  if (!Object.prototype.hasOwnProperty.call(THEMES, theme)) {
    var prefersDark =
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-color-scheme: dark)').matches;
    theme = prefersDark ? 'charcoal' : 'light'; // AUTO_THEMES
  }
  root.setAttribute('data-theme', theme);
  root.setAttribute('data-scheme', THEMES[theme][0]);
  var meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute('content', THEMES[theme][1]);
  if (accent === 'indigo' || accent === 'steel' || accent === 'graphite') {
    root.setAttribute('data-accent', accent);
  }
})();
