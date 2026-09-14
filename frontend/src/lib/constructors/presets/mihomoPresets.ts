import type { ProxyGroup } from '../../mihomoYaml';

export interface MetaRuleSetItem {
  id: string;
  label: string;
  type: 'geosite' | 'geoip';
  defaultOutbound: string;
}

export const META_BASE_URL = 'https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo';

export function buildMetaRuleSetUrl(id: string, type: 'geosite' | 'geoip'): string {
  return `${META_BASE_URL}/${type}/${id}.mrs`;
}

/**
 * Каталог категорий и наборов правил geosite / geoip для Mihomo
 */
export const META_RULE_SETS_BY_CATEGORY: Record<string, MetaRuleSetItem[]> = {
  'Social Networks': [
    { id: 'youtube', label: 'YouTube', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'telegram', label: 'Telegram', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'discord', label: 'Discord', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'twitter', label: 'Twitter/X', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'instagram', label: 'Instagram', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'reddit', label: 'Reddit', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'vk', label: 'VK', type: 'geosite', defaultOutbound: 'DIRECT' },
    { id: 'tiktok', label: 'TikTok', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'twitch', label: 'Twitch', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'facebook', label: 'Facebook', type: 'geosite', defaultOutbound: 'Proxy' }
  ],
  Services: [
    { id: 'spotify', label: 'Spotify', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'steam', label: 'Steam', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'github', label: 'GitHub', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'openai', label: 'OpenAI', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'netflix', label: 'Netflix', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'google', label: 'Google', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'amazon', label: 'Amazon', type: 'geosite', defaultOutbound: 'Proxy' },
    { id: 'speedtest', label: 'Speedtest', type: 'geosite', defaultOutbound: 'Proxy' }
  ],
  'Networks/CDN': [
    { id: 'cloudflare', label: 'Cloudflare', type: 'geosite', defaultOutbound: 'DIRECT' },
    { id: 'akamai', label: 'Akamai', type: 'geosite', defaultOutbound: 'DIRECT' },
    { id: 'fastly', label: 'Fastly', type: 'geosite', defaultOutbound: 'DIRECT' },
    { id: 'digitalocean', label: 'DigitalOcean', type: 'geosite', defaultOutbound: 'DIRECT' },
    { id: 'private', label: 'Private Network', type: 'geoip', defaultOutbound: 'DIRECT' },
    { id: 'telegram', label: 'Telegram IP', type: 'geoip', defaultOutbound: 'Proxy' }
  ],
  Blocked: [
    {
      id: 'category-ads-all',
      label: 'Ads & Trackers',
      type: 'geosite',
      defaultOutbound: 'REJECT'
    },
    {
      id: 'category-ai-!cn',
      label: 'AI Services (non-CN)',
      type: 'geosite',
      defaultOutbound: 'Proxy'
    },
    {
      id: 'category-anticensorship',
      label: 'Anti-Censorship',
      type: 'geosite',
      defaultOutbound: 'Proxy'
    }
  ]
};

export const MIHOMO_GROUP_TYPES = [
  { value: 'select', label: 'Select (Ручной выбор)' },
  { value: 'url-test', label: 'URL-Test (Автовыбор по задержке)' },
  { value: 'fallback', label: 'Fallback (Резервный переключатель)' },
  { value: 'load-balance', label: 'Load-Balance (Балансировка)' }
];

export const ZKEEN_16_GROUPS_TEMPLATE: Omit<ProxyGroup, 'id'>[] = [
  {
    name: 'Blocked Services',
    type: 'select',
    includeAll: true,
    proxies: ['Fallback', 'Fastest'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Reject.png'
  },
  {
    name: 'Fallback',
    type: 'fallback',
    includeAll: true,
    proxies: [],
    hidden: true,
    url: 'https://www.gstatic.com/generate_204',
    interval: 300,
    maxFailedTimes: 3,
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Auto.png'
  },
  {
    name: 'Fastest',
    type: 'url-test',
    includeAll: true,
    proxies: [],
    hidden: true,
    url: 'https://www.gstatic.com/generate_204',
    interval: 300,
    maxFailedTimes: 3,
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Available.png'
  },
  {
    name: 'YouTube',
    type: 'select',
    includeAll: true,
    proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/YouTube.png'
  },
  {
    name: 'Discord',
    type: 'select',
    includeAll: true,
    proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Discord.png'
  },
  {
    name: 'Twitch',
    type: 'select',
    includeAll: true,
    proxies: ['DIRECT', 'Blocked Services', 'Fallback', 'Fastest'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Twitch.png'
  },
  {
    name: 'Reddit',
    type: 'select',
    includeAll: true,
    proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Reddit.png'
  },
  {
    name: 'Social Networks',
    type: 'select',
    includeAll: true,
    proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Instagram.png'
  },
  {
    name: 'Spotify',
    type: 'select',
    includeAll: true,
    proxies: ['DIRECT', 'Blocked Services', 'Fallback', 'Fastest'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Spotify.png'
  },
  {
    name: 'Steam',
    type: 'select',
    includeAll: true,
    proxies: ['DIRECT', 'Blocked Services', 'Fallback', 'Fastest'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Steam.png'
  },
  {
    name: 'Gaming',
    type: 'select',
    includeAll: true,
    proxies: ['DIRECT', 'Blocked Services', 'Fallback', 'Fastest'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Game.png'
  },
  {
    name: 'AI Services',
    type: 'select',
    includeAll: true,
    proxies: ['Blocked Services', 'Fallback', 'Fastest', 'DIRECT'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Bot.png'
  },
  {
    name: 'Crypto',
    type: 'select',
    includeAll: true,
    proxies: ['DIRECT', 'Blocked Services', 'Fallback', 'Fastest'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Bank.png'
  },
  {
    name: 'AdBlock',
    type: 'select',
    includeAll: false,
    proxies: ['REJECT', 'DIRECT'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Advertising.png'
  },
  {
    name: 'Direct Services',
    type: 'select',
    includeAll: false,
    proxies: ['DIRECT', 'Blocked Services'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Direct.png'
  },
  {
    name: 'Other',
    type: 'select',
    includeAll: true,
    proxies: ['DIRECT', 'Blocked Services', 'Fallback', 'Fastest'],
    icon: 'https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/Final.png'
  }
];

export const MIHOMO_PRESETS = {
  metaRuleSetsByCategory: META_RULE_SETS_BY_CATEGORY,
  groupTypes: MIHOMO_GROUP_TYPES,
  zkeen16GroupsTemplate: ZKEEN_16_GROUPS_TEMPLATE,
  baseUrl: META_BASE_URL,
  buildMetaRuleSetUrl
};
