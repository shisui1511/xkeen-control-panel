import { mount } from 'svelte';
import './styles/fonts.css';
import App from './App.svelte';
import { initDensity } from './stores';

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
  const theme = saved || (prefersDark ? 'dark' : 'light');
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

const app = mount(App, {
  target: document.getElementById('app')!
});

export default app;
