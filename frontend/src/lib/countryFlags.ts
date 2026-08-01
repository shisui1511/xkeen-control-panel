// Country Flag Emoji definitions
export const flagMap: Record<string, string> = {
  RU: '🇷🇺',
  Russia: '🇷🇺',
  '\u0420\u043E\u0441\u0441\u0438\u044F': '🇷🇺',
  US: '🇺🇸',
  USA: '🇺🇸',
  '\u0421\u0428\u0410': '🇺🇸',
  GB: '🇬🇧',
  UK: '🇬🇧',
  'United Kingdom': '🇬🇧',
  '\u0410\u043D\u0433\u043B\u0438\u044F': '🇬🇧',
  DE: '🇩🇪',
  Germany: '🇩🇪',
  '\u0413\u0435\u0440\u043C\u0430\u043D\u0438\u044F': '🇩🇪',
  NL: '🇳🇱',
  Netherlands: '🇳🇱',
  '\u041D\u0438\u0434\u0435\u0440\u043B\u0430\u043D\u0434\u044B': '🇳🇱',
  FR: '🇫🇷',
  France: '🇫🇷',
  '\u0424\u0440\u0430\u043D\u0446\u0438\u044F': '🇫🇷',
  FI: '🇫🇮',
  Finland: '🇫🇮',
  '\u0424\u0438\u043D\u043B\u044F\u043D\u0434\u0438\u044F': '🇫🇮',
  TR: '🇹🇷',
  Turkey: '🇹🇷',
  '\u0422\u0443\u0440\u0446\u0438\u044F': '🇹🇷',
  SG: '🇸🇬',
  Singapore: '🇸🇬',
  '\u0421\u0438\u043D\u0433\u0430\u04FF\u0443\u0440': '🇸🇬',
  JP: '🇯🇵',
  Japan: '🇯🇵',
  '\u042F\u043F\u043E\u043D\u0438\u044F': '🇯🇵',
  HK: '🇭🇰',
  'Hong Kong': '🇭🇰',
  '\u0413\u043E\u043D\u043A\u043E\u043D\u0433': '🇭🇰',
  TW: '🇹🇼',
  Taiwan: '🇹🇼',
  '\u0422\u0430\u0439\u0432\u0430\u043D\u044C': '🇹🇼',
  KR: '🇰🇷',
  'South Korea': '🇰🇷',
  '\u041A\u043E\u0440\u0435\u044F': '🇰🇷',
  Seoul: '🇰🇷',
  IN: '🇮🇳',
  India: '🇮🇳',
  '\u0418\u043D\u0434\u0438\u044F': '🇮🇳',
  BR: '🇧🇷',
  Brazil: '🇧🇷',
  '\u0411\u0440\u0430\u0437\u0438\u043B\u0438\u044F': '🇧🇷'
};

export function getCountryFlag(nodeName: string): string {
  const lower = nodeName.toLowerCase();
  for (const [key, emoji] of Object.entries(flagMap)) {
    const escapedKey = key.replace(/[-/\\^$*+?.()|[\]{}]/g, '\\$&');
    const regex = new RegExp(`\\b${escapedKey}\\b|${escapedKey}`, 'i');
    if (regex.test(lower)) {
      return emoji;
    }
  }
  return '';
}
