/// <reference types="vite/client" />

import 'svelte/elements';

declare module 'svelte/elements' {
  interface HTMLAttributes<T> {
    inert?: boolean | undefined | null;
  }
}

declare module '*.svelte' {
  import type { ComponentType } from 'svelte';
  const component: ComponentType;
  export default component;
}

