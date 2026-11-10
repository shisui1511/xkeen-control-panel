import { derived } from 'svelte/store';
import { t } from '../i18n';
import { activeKernelName, mihomoOfflineReason, runningKernels } from '../stores';

// Общая логика пустого состояния «Mihomo недоступен» страниц «Прокси» и «Подключения».

/**
 * Запуск Mihomo со страницы — только когда не работает ни одно ядро: при активном Xray
 * (или любом другом запущенном процессе ядра) запуск столкнул бы два ядра (FND-05).
 */
export const canLaunchMihomo = derived(
  [activeKernelName, runningKernels],
  ([$active, $running]) => $active !== 'xray' && $running.length === 0
);

/** Подпись пустого состояния, когда запуск не предлагается: причина по ядру и API. */
export const mihomoOfflineDesc = derived(
  [t, activeKernelName, mihomoOfflineReason],
  ([$t, $active, $reason]) =>
    $active === 'xray'
      ? $t('apioffline.reason_xray')
      : $reason === 'api_down'
        ? $t('apioffline.reason_api_down')
        : $t('ds.empty.mihomo_offline_desc')
);
