import { writable } from 'svelte/store'

// UI state: controls whether the off-canvas sidebar is visible on mobile
export const isSidebarOpen = writable(false)
