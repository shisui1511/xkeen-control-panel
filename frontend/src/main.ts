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

initTheme();
initDensity();

const app = mount(App, {
  target: document.getElementById('app')!
});

export default app;
