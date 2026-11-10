import { derived, get } from 'svelte/store';
import { configLayerEnabled, configLayerFlagKnown } from '../stores';

// Адреса Редактора и конструкторов с учётом флага слоя конфигурации (D-01, D-16).
// При включённом слое Редактор ведёт во вкладку «Файлы» единого раздела,
// а конструкторы — во вкладку секций ядер «Xray │ Mihomo».
// Пока флаг неизвестен или выключен — прежние адреса v0.29.0.

/** Адрес Редактора как стор: разметка (href) обновляется, когда флаг слоя стал известен позже. */
export const editorHref = derived([configLayerFlagKnown, configLayerEnabled], ([known, enabled]) =>
  known && enabled ? '#/config/files' : '#/editor'
);

/** Текущий адрес Редактора для обработчиков событий (разовое чтение). */
export function editorHash(): string {
  return get(editorHref);
}

export function constructorHash(): string {
  return get(configLayerFlagKnown) && get(configLayerEnabled) ? '#/config/cores' : '#/constructor';
}
