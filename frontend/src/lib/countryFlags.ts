// Country Flag & Node Name Utilities for XKeen Control Panel

// Unicode Regional Indicator Symbol pair to Flag Emoji and ISO Country Code
export function countryCodeToFlag(code: string): string {
  const upper = String(code || '')
    .trim()
    .toUpperCase();
  if (!/^[A-Z]{2}$/.test(upper)) return '';
  const r1 = 0x1f1e6 + upper.charCodeAt(0) - 65;
  const r2 = 0x1f1e6 + upper.charCodeAt(1) - 65;
  return String.fromCodePoint(r1, r2);
}

// Extracts flag emoji from string if present (Unicode Regional Indicator pair)
export function extractFlagEmoji(s: string): { flag: string; countryCode: string } | null {
  const chars = Array.from(String(s || ''));
  for (let i = 0; i < chars.length - 1; i++) {
    const cp1 = chars[i].codePointAt(0);
    const cp2 = chars[i + 1].codePointAt(0);
    if (
      cp1 !== undefined &&
      cp2 !== undefined &&
      cp1 >= 0x1f1e6 &&
      cp1 <= 0x1f1ff &&
      cp2 >= 0x1f1e6 &&
      cp2 <= 0x1f1ff
    ) {
      const code = String.fromCharCode(65 + cp1 - 0x1f1e6, 65 + cp2 - 0x1f1e6);
      return { flag: chars[i] + chars[i + 1], countryCode: code };
    }
  }
  return null;
}

// 2-letter ISO Country Code names for verification and labels
export const COUNTRY_NAMES: Record<string, string> = Object.freeze({
  AD: 'Andorra',
  AE: 'United Arab Emirates',
  AF: 'Afghanistan',
  AG: 'Antigua and Barbuda',
  AL: 'Albania',
  AM: 'Armenia',
  AO: 'Angola',
  AR: 'Argentina',
  AT: 'Austria',
  AU: 'Australia',
  AZ: 'Azerbaijan',
  BA: 'Bosnia and Herzegovina',
  BB: 'Barbados',
  BD: 'Bangladesh',
  BE: 'Belgium',
  BF: 'Burkina Faso',
  BG: 'Bulgaria',
  BH: 'Bahrain',
  BI: 'Burundi',
  BJ: 'Benin',
  BN: 'Brunei',
  BO: 'Bolivia',
  BR: 'Brazil',
  BS: 'Bahamas',
  BT: 'Bhutan',
  BW: 'Botswana',
  BY: 'Belarus',
  BZ: 'Belize',
  CA: 'Canada',
  CD: 'DR Congo',
  CF: 'Central African Republic',
  CG: 'Congo',
  CH: 'Switzerland',
  CI: 'Ivory Coast',
  CL: 'Chile',
  CM: 'Cameroon',
  CN: 'China',
  CO: 'Colombia',
  CR: 'Costa Rica',
  CU: 'Cuba',
  CV: 'Cape Verde',
  CY: 'Cyprus',
  CZ: 'Czechia',
  DE: 'Germany',
  DJ: 'Djibouti',
  DK: 'Denmark',
  DM: 'Dominica',
  DO: 'Dominican Republic',
  DZ: 'Algeria',
  EC: 'Ecuador',
  EE: 'Estonia',
  EG: 'Egypt',
  ES: 'Spain',
  ET: 'Ethiopia',
  FI: 'Finland',
  FJ: 'Fiji',
  FR: 'France',
  GA: 'Gabon',
  GB: 'United Kingdom',
  GD: 'Grenada',
  GE: 'Georgia',
  GH: 'Ghana',
  GM: 'Gambia',
  GN: 'Guinea',
  GQ: 'Equatorial Guinea',
  GR: 'Greece',
  GT: 'Guatemala',
  GW: 'Guinea-Bissau',
  GY: 'Guyana',
  HK: 'Hong Kong',
  HN: 'Honduras',
  HR: 'Croatia',
  HT: 'Haiti',
  HU: 'Hungary',
  ID: 'Indonesia',
  IE: 'Ireland',
  IL: 'Israel',
  IN: 'India',
  IQ: 'Iraq',
  IR: 'Iran',
  IS: 'Iceland',
  IT: 'Italy',
  JM: 'Jamaica',
  JO: 'Jordan',
  JP: 'Japan',
  KE: 'Kenya',
  KG: 'Kyrgyzstan',
  KH: 'Cambodia',
  KI: 'Kiribati',
  KM: 'Comoros',
  KN: 'Saint Kitts and Nevis',
  KP: 'North Korea',
  KR: 'South Korea',
  KW: 'Kuwait',
  KZ: 'Kazakhstan',
  LA: 'Laos',
  LB: 'Lebanon',
  LC: 'Saint Lucia',
  LI: 'Liechtenstein',
  LK: 'Sri Lanka',
  LR: 'Liberia',
  LS: 'Lesotho',
  LT: 'Lithuania',
  LU: 'Luxembourg',
  LV: 'Latvia',
  LY: 'Libya',
  MA: 'Morocco',
  MC: 'Monaco',
  MD: 'Moldova',
  ME: 'Montenegro',
  MG: 'Madagascar',
  MK: 'North Macedonia',
  ML: 'Mali',
  MM: 'Myanmar',
  MN: 'Mongolia',
  MO: 'Macao',
  MR: 'Mauritania',
  MT: 'Malta',
  MU: 'Mauritius',
  MV: 'Maldives',
  MW: 'Malawi',
  MX: 'Mexico',
  MY: 'Malaysia',
  MZ: 'Mozambique',
  NA: 'Namibia',
  NE: 'Niger',
  NG: 'Nigeria',
  NI: 'Nicaragua',
  NL: 'Netherlands',
  NO: 'Norway',
  NP: 'Nepal',
  NR: 'Nauru',
  NZ: 'New Zealand',
  OM: 'Oman',
  PA: 'Panama',
  PE: 'Peru',
  PG: 'Papua New Guinea',
  PH: 'Philippines',
  PK: 'Pakistan',
  PL: 'Poland',
  PT: 'Portugal',
  PY: 'Paraguay',
  QA: 'Qatar',
  RO: 'Romania',
  RS: 'Serbia',
  RU: 'Russia',
  RW: 'Rwanda',
  SA: 'Saudi Arabia',
  SB: 'Solomon Islands',
  SC: 'Seychelles',
  SD: 'Sudan',
  SE: 'Sweden',
  SG: 'Singapore',
  SI: 'Slovenia',
  SK: 'Slovakia',
  SL: 'Sierra Leone',
  SM: 'San Marino',
  SN: 'Senegal',
  SO: 'Somalia',
  SR: 'Suriname',
  SS: 'South Sudan',
  ST: 'Sao Tome and Principe',
  SV: 'El Salvador',
  SY: 'Syria',
  SZ: 'Eswatini',
  TD: 'Chad',
  TG: 'Togo',
  TH: 'Thailand',
  TJ: 'Tajikistan',
  TL: 'Timor-Leste',
  TM: 'Turkmenistan',
  TN: 'Tunisia',
  TO: 'Tonga',
  TR: 'Turkey',
  TT: 'Trinidad and Tobago',
  TV: 'Tuvalu',
  TW: 'Taiwan',
  TZ: 'Tanzania',
  UA: 'Ukraine',
  UG: 'Uganda',
  US: 'United States',
  UY: 'Uruguay',
  UZ: 'Uzbekistan',
  VA: 'Vatican City',
  VC: 'Saint Vincent and the Grenadines',
  VE: 'Venezuela',
  VN: 'Vietnam',
  VU: 'Vanuatu',
  WS: 'Samoa',
  YE: 'Yemen',
  ZA: 'South Africa',
  ZM: 'Zambia',
  ZW: 'Zimbabwe'
});

// Common 3-letter / informal code aliases to 2-letter ISO
const CODE_ALIASES: Record<string, string> = {
  UK: 'GB',
  UAE: 'AE',
  USA: 'US',
  SH: 'CH',
  RUS: 'RU',
  DEU: 'DE',
  NLD: 'NL',
  FRA: 'FR',
  JPN: 'JP',
  KOR: 'KR',
  SGP: 'SG',
  HKG: 'HK',
  TWN: 'TW',
  SWE: 'SE',
  NOR: 'NO',
  FIN: 'FI',
  POL: 'PL',
  CZE: 'CZ',
  TUR: 'TR',
  KAZ: 'KZ',
  UKR: 'UA',
  BLR: 'BY',
  MDA: 'MD',
  GEO: 'GE',
  ARM: 'AM',
  ISR: 'IL',
  BRA: 'BR',
  CAN: 'CA',
  AUS: 'AU',
  BGR: 'BG',
  CYP: 'CY',
  EST: 'EE',
  LVA: 'LV',
  LTU: 'LT',
  ROU: 'RO',
  SRB: 'RS',
  GRC: 'GR',
  PRT: 'PT',
  IRL: 'IE',
  ISL: 'IS',
  BEL: 'BE',
  DNK: 'DK',
  HUN: 'HU',
  SVK: 'SK',
  SVN: 'SI',
  HRV: 'HR',
  ARG: 'AR',
  CHL: 'CL',
  ZAF: 'ZA',
  NZL: 'NZ',
  VNM: 'VN',
  THA: 'TH',
  MYS: 'MY',
  IDN: 'ID',
  CHN: 'CN',
  UZB: 'UZ',
  AZE: 'AZ',
  KGZ: 'KG',
  TJK: 'TJ'
};

// Helper to create unicode-aware word boundary pattern
function uRule(pattern: string): RegExp {
  return new RegExp(`(?:^|[^\\p{L}\\p{N}])(?:${pattern})(?:$|[^\\p{L}\\p{N}])`, 'iu');
}

// Regex patterns for country and city detection in node names
const COUNTRY_MATCH_RULES: readonly [RegExp, string][] = Object.freeze([
  // Russia
  [
    uRule(
      'russia|russian|moscow|saint\\s*petersburg|spb|россия|российск\\w*|москва|санкт[- ]петербург|питер|новосибирск'
    ),
    'RU'
  ],
  // United States
  [
    uRule(
      'united\\s*states|usa|america|new\\s*york|los\\s*angeles|chicago|miami|dallas|seattle|atlanta|san\\s*jose|ashburn|california|texas|virginia|сша|америк\\w*|чикаго|нью[- ]йорк'
    ),
    'US'
  ],
  // Germany
  [
    uRule(
      'germany|deutschland|frankfurt|berlin|munich|dusseldorf|германия|немецк\\w*|франкфурт|берлин|мюнхен|дюссельдорф'
    ),
    'DE'
  ],
  // Netherlands
  [uRule('netherlands|holland|amsterdam|rotterdam|нидерланды|голландия|амстердам|роттердам'), 'NL'],
  // United Kingdom
  [
    uRule(
      'united\\s*kingdom|great\\s*britain|england|london|manchester|великобритания|англия|лондон|манчестер'
    ),
    'GB'
  ],
  // France
  [uRule('france|paris|marseille|франция|париж|марсель'), 'FR'],
  // Finland
  [uRule('finland|helsinki|финляндия|хельсинки'), 'FI'],
  // Sweden
  [uRule('sweden|stockholm|швеция|стокгольм'), 'SE'],
  // Norway
  [uRule('norway|oslo|норвегия|осло'), 'NO'],
  // Poland
  [uRule('poland|warsaw|krakow|польша|варшава|краков'), 'PL'],
  // Czechia
  [uRule('czechia|czech\\s*republic|prague|praha|чехия|прага'), 'CZ'],
  // Switzerland
  [uRule('switzerland|swiss|zurich|zürich|geneva|швейцария|цюрих|женева'), 'CH'],
  // Austria
  [uRule('austria|vienna|wien|австрия|вена'), 'AT'],
  // Italy
  [uRule('italy|italia|milan|milano|rome|roma|италия|милан|рим'), 'IT'],
  // Spain
  [uRule('spain|espana|madrid|barcelona|испания|мадрид|барселона'), 'ES'],
  // Turkey
  [uRule('turkey|türkiye|istanbul|ankara|antalya|турция|стамбул|анкара|анталья'), 'TR'],
  // Kazakhstan
  [uRule('kazakhstan|almaty|astana|казахстан|алматы|астана'), 'KZ'],
  // Ukraine
  [uRule('ukraine|kyiv|kiev|украина|киев'), 'UA'],
  // Belarus
  [uRule('belarus|minsk|беларусь|белоруссия|минск'), 'BY'],
  // Moldova
  [uRule('moldova|chisinau|молдова|кишинев|молдавия'), 'MD'],
  // Georgia
  [uRule('georgia|tbilisi|грузия|тбилиси'), 'GE'],
  // Armenia
  [uRule('armenia|yerevan|армения|ереван'), 'AM'],
  // Israel
  [uRule('israel|tel\\s*aviv|jerusalem|израиль|тель[- ]авив|иерусалим'), 'IL'],
  // United Arab Emirates
  [uRule('uae|emirates|dubai|abu\\s*dhabi|оаэ|эмираты|дубай|абу[- ]даби'), 'AE'],
  // Singapore
  [uRule('singapore|сингапур'), 'SG'],
  // Japan
  [uRule('japan|tokyo|osaka|япония|токио|осака'), 'JP'],
  // Hong Kong
  [uRule('hong\\s*kong|hkg|гонконг'), 'HK'],
  // South Korea
  [uRule('south\\s*korea|korea|seoul|корея|сеул'), 'KR'],
  // Taiwan
  [uRule('taiwan|taipei|тайвань|тайбэй'), 'TW'],
  // India
  [uRule('india|mumbai|delhi|bangalore|индия|мумбаи|дели'), 'IN'],
  // Brazil
  [uRule('brazil|brasil|sao\\s*paulo|бразилия|сан[- ]паулу'), 'BR'],
  // Canada
  [uRule('canada|toronto|montreal|vancouver|канада|торонто|монреаль|ванкувер'), 'CA'],
  // Australia
  [uRule('australia|sydney|melbourne|австралия|сидней|мельбурн'), 'AU'],
  // Bulgaria
  [uRule('bulgaria|sofia|болгария|софия'), 'BG'],
  // Cyprus
  [uRule('cyprus|nicosia|limassol|кипр|никосия|лимассол'), 'CY'],
  // Estonia
  [uRule('estonia|tallinn|эстония|таллин'), 'EE'],
  // Latvia
  [uRule('latvia|riga|латвия|рига'), 'LV'],
  // Lithuania
  [uRule('lithuania|vilnius|литва|вильнюс'), 'LT'],
  // Romania
  [uRule('romania|bucharest|румыния|бухарест'), 'RO'],
  // Serbia
  [uRule('serbia|belgrade|сербия|белград'), 'RS'],
  // Greece
  [uRule('greece|athens|греция|афины'), 'GR'],
  // Portugal
  [uRule('portugal|lisbon|португалия|лиссабон'), 'PT'],
  // Ireland
  [uRule('ireland|dublin|ирландия|дублин'), 'IE'],
  // Iceland
  [uRule('iceland|reykjavik|исландия|рейкьявик'), 'IS'],
  // Belgium
  [uRule('belgium|brussels|бельгия|брюссель'), 'BE'],
  // Denmark
  [uRule('denmark|copenhagen|дания|копенгаген'), 'DK'],
  // Hungary
  [uRule('hungary|budapest|венгрия|будапешт'), 'HU'],
  // Slovakia
  [uRule('slovakia|bratislava|словакия|братислава'), 'SK'],
  // Slovenia
  [uRule('slovenia|ljubljana|словения|любляна'), 'SI'],
  // Croatia
  [uRule('croatia|zagreb|хорватия|загреб'), 'HR'],
  // Argentina
  [uRule('argentina|buenos\\s*aires|аргентина'), 'AR'],
  // Chile
  [uRule('chile|santiago|чили|сантьяго'), 'CL'],
  // South Africa
  [uRule('south\\s*africa|johannesburg|cape\\s*town|юар'), 'ZA'],
  // New Zealand
  [uRule('new\\s*zealand|auckland|новая\\s*зеландия'), 'NZ'],
  // Vietnam
  [uRule('vietnam|hanoi|вьетнам|ханой'), 'VN'],
  // Thailand
  [uRule('thailand|bangkok|таиланд|тайланд|бангкок'), 'TH'],
  // Malaysia
  [uRule('malaysia|kuala\\s*lumpur|малайзия'), 'MY'],
  // Indonesia
  [uRule('indonesia|jakarta|индонезия|джакарта'), 'ID'],
  // China
  [uRule('china|beijing|shanghai|shenzhen|guangzhou|китай|пекин|шанхай'), 'CN'],
  // Uzbekistan
  [uRule('uzbekistan|tashkent|узбекистан|ташкент'), 'UZ'],
  // Azerbaijan
  [uRule('azerbaijan|baku|азербайджан|баку'), 'AZ'],
  // Kyrgyzstan
  [uRule('kyrgyzstan|bishkek|киргизия|кыргызстан|бишкек'), 'KG'],
  // Tajikistan
  [uRule('tajikistan|dushanbe|таджикистан|душанбе'), 'TJ']
]);

/**
 * Resolves 2-letter ISO Country Code from node name.
 */
export function getCountryCode(nodeName: string): string {
  const value = String(nodeName || '').trim();
  if (!value) return '';

  // 1. Check if nodeName contains existing flag emoji
  const extracted = extractFlagEmoji(value);
  if (extracted && COUNTRY_NAMES[extracted.countryCode]) {
    return extracted.countryCode;
  }

  // 2. Check for bracketed codes like [US], (DE), [NL-01]
  const bracketMatch = value.match(/^[[(]([A-Za-z]{2,3})(?:[-_0-9\s]*)?[\])]/);
  if (bracketMatch) {
    const raw = bracketMatch[1].toUpperCase();
    const code = CODE_ALIASES[raw] || raw;
    if (COUNTRY_NAMES[code]) return code;
  }

  // 3. Check for leading token like "US - ", "DE_01", "NL: VIP", "RU 02"
  const normalized = value.replace(/[_./:|•-]+/g, ' ').trim();
  const tokenMatch = normalized.match(/^([A-Za-z]{2,3})(?=\s|$)/);
  if (tokenMatch) {
    const raw = tokenMatch[1].toUpperCase();
    const code = CODE_ALIASES[raw] || raw;
    if (COUNTRY_NAMES[code]) return code;
  }

  // 4. Check dictionary of country and city names
  for (const [rule, code] of COUNTRY_MATCH_RULES) {
    if (rule.test(value)) {
      return code;
    }
  }

  return '';
}

/**
 * Checks if a string already contains a flag emoji.
 */
export function hasFlagEmoji(s: string): boolean {
  return extractFlagEmoji(s) !== null;
}

/**
 * Gets flag emoji for a node name. Returns '' if no country matched.
 */
export function getCountryFlag(nodeName: string): string {
  const code = getCountryCode(nodeName);
  return code ? countryCodeToFlag(code) : '';
}

/**
 * Returns flag emoji ONLY if nodeName doesn't already contain one.
 * Prevents duplicating flags when provider already included one.
 */
export function getMissingCountryFlag(nodeName: string): string {
  if (hasFlagEmoji(nodeName)) {
    return '';
  }
  return getCountryFlag(nodeName);
}

/**
 * Cleans the node name for display by stripping leading/trailing flag emojis
 * and redundant punctuation, avoiding duplicate flags (e.g. "🇺🇸 🇺🇸 США • Чикаго" -> "США • Чикаго").
 */
export function cleanNodeDisplayName(nodeName: string): string {
  let value = String(nodeName || '').trim();
  if (!value) return '';

  // Strip leading flag emojis (Regional Indicator Symbols) and variations
  value = value.replace(/^(?:(?:[\u{1F1E6}-\u{1F1FF}]{2})|\uFE0F|\u200D|\s)+/gu, '').trim();

  // Strip trailing flag emojis
  value = value.replace(/(?:(?:[\u{1F1E6}-\u{1F1FF}]{2})|\uFE0F|\u200D|\s)+$/gu, '').trim();

  // Clean redundant punctuation left at the start or end
  value = value
    .replace(/^[-_/:|•\s]+/, '')
    .replace(/[-_/:|•\s]+$/, '')
    .trim();

  return value || String(nodeName || '').trim();
}

/**
 * Backward compatibility: flagMap mapping key to emoji.
 */
export const flagMap: Record<string, string> = new Proxy(
  {},
  {
    get(_target, prop: string | symbol) {
      if (typeof prop !== 'string') return '';
      return getCountryFlag(prop);
    },
    has() {
      return true;
    }
  }
);
