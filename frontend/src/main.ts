import { mount } from 'svelte';
import './styles/fonts.css';
import App from './App.svelte';

function initTheme() {
  let saved = '';
  try {
    saved = localStorage.getItem('theme') || '';
  } catch (e) {
    // localStorage may be unavailable in private mode or with blocked cookies
  }
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  const theme = saved || (prefersDark ? 'dark' : 'light');
  document.documentElement.setAttribute('data-theme', theme);
}

function initDensity() {
  let saved = '';
  try {
    saved = localStorage.getItem('theme_density') || '';
  } catch (e) {
    // localStorage may be unavailable
  }
  if (saved === 'compact' || saved === 'comfortable') {
    document.documentElement.setAttribute('data-density', saved);
  } else {
    const isMobile = window.matchMedia('(max-width: 1024px)').matches;
    document.documentElement.setAttribute('data-density', isMobile ? 'compact' : 'comfortable');
  }
}

initTheme();
initDensity();

const app = mount(App, {
  target: document.getElementById('app')!
});

export default app;
