import { mount } from 'svelte';
import './styles/fonts.css';
import App from './App.svelte';
import { initDensity } from './stores';
import { registerServiceWorker } from './sw-register';

function initTheme() {
  let saved = '';
  try {
    saved = localStorage.getItem('theme') || '';
  } catch (e) {
    // localStorage may be unavailable in private mode or with blocked cookies
  }
  const prefersDark =
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches;
  // Белый список как в public/theme-init.js: произвольное значение не попадает в атрибут.
  const theme = saved === 'light' || saved === 'dark' ? saved : prefersDark ? 'dark' : 'light';
  document.documentElement.setAttribute('data-theme', theme);
}

function initAccent() {
  let saved = '';
  try {
    saved = localStorage.getItem('accent') || '';
  } catch (e) {
    // localStorage may be unavailable in private mode or with blocked cookies
  }
  if (saved === 'indigo' || saved === 'steel' || saved === 'graphite') {
    document.documentElement.setAttribute('data-accent', saved);
  } else {
    document.documentElement.removeAttribute('data-accent');
  }
}

initTheme();
initAccent();
initDensity();
registerServiceWorker();

const app = mount(App, {
  target: document.getElementById('app')!
});

export default app;
