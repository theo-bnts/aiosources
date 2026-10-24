import { debridProviders } from './debrid.ts'
import { runtimeSections, type RuntimeSection } from './runtime-settings-definitions.ts'
import { importNumberFields } from './import-settings-definitions.ts'

export type SettingsEntity =
  'source' | 'remote' | 'stremio' | 'torznab' | 'share' | 'proxy' | 'tracker' | 'source-route'
export type Setting = {
  id: string
  title: string
  group: string
  route: string
  // Targets describe controls, never their values. Labels also cover generated React IDs.
  control?: string
  label?: string
  fallback?: string
  reveal?: string[]
  keywords?: string
  entity?: SettingsEntity
  stage?: string
  adapters?: string[]
  public?: 'search' | 'stremio' | 'torznab' | 'interface'
}

const settings: Setting[] = []
const importCategory = (key: string) =>
  key === 'workers'
    ? 'Concurrency'
    : key.startsWith('max_quota')
      ? 'Quota handling'
      : 'Useful progress'
const providerSettingSuffix = (id: string) =>
  id === 'alldebrid' ? 'ad' : id === 'torbox' ? 'tb' : id
function add(
  base: Omit<Setting, 'id' | 'title'>,
  fields: readonly (readonly [string, string, string?])[],
) {
  for (const [id, title, keywords] of fields)
    settings.push({
      ...base,
      id,
      title,
      keywords: [base.keywords, keywords].filter(Boolean).join(' '),
      label: title,
    })
}

const runtimeLocations: Record<RuntimeSection, [string, string]> = {
  debrid: ['/debrid/settings', 'Debrid'],
  debrid_retries: ['/debrid/settings', 'Debrid'],
  outgoing: ['/network/outgoing', 'Administration / Network / Outgoing'],
  connections: ['/network/incoming', 'Administration / Network / Incoming'],
  downloads: ['/network/incoming', 'Administration / Network / Incoming'],
  payloads: ['/network/incoming', 'Administration / Network / Incoming'],
  history: ['/health/settings', 'Administration / Storage'],
  security: ['/security/settings', 'Administration / Security'],
  source_retries: ['/providers/settings?settings=advanced', 'Sources / Sources'],
  identification: ['/metadata/settings', 'Metadata / Metadata'],
  matching: ['/metadata/settings', 'Metadata / Metadata'],
  metadata: ['/metadata/settings', 'Metadata / Metadata'],
  metadata_retries: ['/metadata/settings', 'Metadata / Metadata'],
}
for (const section of Object.keys(runtimeSections) as RuntimeSection[]) {
  const definition = runtimeSections[section]
  const [route, group] = runtimeLocations[section]
  for (const [key, title] of definition.fields)
    settings.push({
      id: `runtime.${section}.${key}`,
      title,
      route,
      group: `${group} / ${definition.title}`,
      control: `runtime-${section}-${key}`,
      reveal: ['Advanced', definition.title],
      keywords: `${section.replaceAll('_', ' ')} ${key.replaceAll('_', ' ')} ${definition.help}`,
    })
}
for (const [key, field] of Object.entries(importNumberFields))
  settings.push({
    id: `imports.${key}`,
    title: field.label,
    route: '/providers/settings?settings=advanced',
    group: `Sources / Sources / Import execution / ${importCategory(key)}`,
    control: `collection-${key}`,
    reveal: ['Advanced', 'Import execution', importCategory(key)],
    keywords: field.help,
  })
add(
  {
    route: '/providers/settings?settings=advanced',
    group: 'Sources / Sources / Import execution',
    reveal: ['Advanced', 'Import execution'],
  },
  [
    ['imports.progress', 'At the no-progress threshold', 'pause stop no progress'],
    [
      'imports.recovery',
      'Automatically resume interrupted imports',
      'recovery restart checkpoint reprise redemarrage',
    ],
  ],
)
add(
  {
    route: '/metadata/settings',
    group: 'Metadata / Metadata / Settings',
    keywords: 'TMDB metadata identification metadonnees',
  },
  [
    ['metadata.key', 'TMDB read access token', 'API key token cle'],
    ['metadata.rate', 'TMDB requests per second', 'rate limit requests requetes'],
    ['metadata.sync', 'Automatically update film & series information'],
    [
      'metadata.interval',
      'Information update interval (minutes)',
      'schedule frequency cadence intervalle',
    ],
    ['metadata.match', 'Automatically identify torrents'],
    [
      'metadata.match-interval',
      'Identification interval (seconds)',
      'matching schedule frequency cadence intervalle',
    ],
  ],
)
add({ route: '/debrid/settings', group: 'Debrid / Settings' }, [
  ...debridProviders.map(
    (p) =>
      [
        `debrid.${providerSettingSuffix(p.id)}`,
        `${p.name} API key`,
        'credential token cle',
      ] as const,
  ),
  ['debrid.cache', 'Cache validity (minutes)', 'availability expiration cache'],
])
add(
  {
    route: '/debrid/settings',
    group: 'Debrid / Private source playback',
    entity: 'tracker',
    reveal: ['Advanced', 'Private source playback'],
  },
  [
    ['tracker.private', 'Private source', 'private tracker privee'],
    ['tracker.announce', 'Announce URL secret', 'passkey tracker announce annonce'],
  ],
)
add(
  {
    route: '/health/settings',
    group: 'Administration / Storage / Settings',
    keywords: 'storage stockage disk espace retention',
  },
  [['storage.mode', 'Stored torrent information', 'compact complete space']],
)
settings.push({
  id: 'storage.reset',
  title: 'Reset all source data',
  route: '/health/settings',
  group: 'Administration / Storage / Maintenance',
  label: 'Reset all source data',
  reveal: ['Advanced', 'Maintenance'],
  keywords: 'reset delete reinitialiser supprimer maintenance',
})
add(
  {
    route: '/network/incoming',
    group: 'Administration / Network / Incoming / Trusted proxy',
    keywords: 'cloudflare tunnel ip header entete reseau proxy confiance',
  },
  [
    ['incoming.enabled', 'Trust an incoming proxy header'],
    ['incoming.header', 'IP address header'],
    ['incoming.format', 'Header format'],
    ['incoming.proxies', 'Trusted proxy addresses', 'CIDR networks'],
  ],
)
add(
  {
    route: '/public-access/limits',
    group: 'Administration / Public access / Request limits',
    keywords: 'public rate limit quota limite requetes',
  },
  [
    ['limits.setup', 'Link creations per minute'],
    ['limits.requests', 'Public requests per minute'],
    ['limits.concurrent', 'Simultaneous public requests'],
  ],
)
add(
  {
    route: '/public-access',
    group: 'Administration / Public access / Settings',
    keywords: 'public access acces',
  },
  [
    ['public.enabled', 'Enable public access'],
    ['public.search', 'Allow public Search'],
    ['public.stremio', 'Allow public Stremio connections'],
    ['public.torznab', 'Allow public Torznab connections'],
    [
      'public.sources',
      'Expose source names and filters',
      'announce private privacy names sources visible visibilite',
    ],
  ],
)
add(
  {
    route: '/network/outgoing/routing',
    group: 'Administration / Network / Outgoing / Routing',
    keywords: 'proxy VPN WARP route reseau connexion',
  },
  [
    ['routing.default', 'Outgoing connections', 'default connection'],
    ['routing.tmdb', 'TMDB'],
    ...debridProviders.map((p) => [`routing.${providerSettingSuffix(p.id)}`, p.name] as const),
    ['routing.downloads', 'Browser downloads'],
  ],
)
settings.push({
  id: 'routing.source',
  title: 'Source connection',
  route: '/network/outgoing/routing',
  group: 'Administration / Network / Outgoing / Routing',
  entity: 'source-route',
  keywords: 'source proxy VPN WARP routing',
})
add(
  {
    route: '/network/outgoing',
    group: 'Administration / Network / Outgoing / Proxies',
    entity: 'proxy',
    keywords: 'proxy VPN WARP SOCKS HTTP reseau',
  },
  [
    ['proxy.name', 'Name'],
    ['proxy.url', 'Proxy URL'],
    ['proxy.username', 'Username'],
    ['proxy.password', 'Password'],
  ],
)
for (const setting of settings.filter(
  (entry) => entry.entity === 'proxy' && ['Username', 'Password'].includes(entry.title),
))
  setting.reveal = ['Advanced']
settings.push(
  {
    id: 'security.secrets',
    title: 'Secrets',
    route: '/security/settings',
    group: 'Administration / Security / Settings',
    control: 'secrets',
    keywords: 'password credential key token mot de passe cle',
  },
  {
    id: 'security.mfa',
    title: 'Two-step verification',
    route: '/security/settings',
    group: 'Administration / Security / Settings',
    control: 'verification',
    keywords: '2FA MFA TOTP authenticator recovery codes securite authentification',
  },
  {
    id: 'security.sessions',
    title: 'Active sessions',
    route: '/security/settings',
    group: 'Administration / Security / Settings',
    control: 'sessions',
    keywords: 'devices sign out appareils deconnexion',
  },
)
for (const [id, title] of [
  ['page-size', 'Rows per page'],
  ['refresh', 'Automatic refresh'],
  ['date-format', 'Date format'],
  ['time-zone', 'Time zone'],
] as const)
  settings.push({
    id: `interface.${id}`,
    title,
    route: '',
    group: 'Interface preferences',
    control: `interface-${id}`,
    public: 'interface',
    keywords:
      'browser preferences display pagination dates time refresh navigateur affichage fuseau rafraichissement',
  })

add(
  {
    route: '/remotes/settings',
    group: 'Sources / Remote catalogs / Settings',
    entity: 'remote',
    keywords: 'remote catalog catalogue distant sync import',
  },
  [
    ['remote.id', 'Local source identifier'],
    ['remote.name', 'Display name'],
    ['remote.url', 'HTTPS catalog URL'],
    ['remote.password', 'Sharing password'],
    ['remote.enabled', 'Connection enabled'],
    ['remote.metadata', 'Film and series information', 'TMDB titles translations additional data'],
    ['remote.availability', 'Debrid cache observations', 'shared availability additional data'],
    ['remote.auto', 'Automatic synchronization'],
    ['remote.interval', 'Synchronization interval (minutes)'],
  ],
)
for (const [entity, route, name] of [
  ['stremio', '/addon/settings', 'Stremio'],
  ['torznab', '/torznab', 'Torznab'],
  ['share', '/sharing/settings', 'Shared catalogs'],
] as const) {
  const group = `Connections / ${name}`
  add({ route, group: `${group} / Settings`, entity }, [
    [`${entity}.name`, entity === 'share' ? 'Recipient / share name' : 'Access name'],
  ])
  add(
    { route, group: `${group} / Filters`, entity, reveal: ['Filters'] },
    entity === 'share'
      ? [
          [`${entity}.sources`, 'Sources'],
          [`${entity}.fields`, 'Safe fields to share'],
        ]
      : [
          [`${entity}.media`, 'Media types'],
          [`${entity}.sources`, 'Sources'],
          [`${entity}.cached`, 'Cached results only'],
          [`${entity}.services`, 'Services'],
        ],
  )
  if (entity === 'share')
    add({ route, group: `${group} / Additional data`, entity, reveal: ['Additional data'] }, [
      ['share.metadata', 'Film and series information', 'TMDB titles translations'],
      ['share.availability', 'Debrid cache observations', 'shared availability'],
    ])
  if (entity !== 'share') {
    add({ route, group: `${group} / Filters`, entity, reveal: ['Filters'] }, [
      [`${entity}.max-results`, 'Maximum results'],
      [`${entity}.max-cached`, 'Maximum cached results'],
      [
        `${entity}.response`,
        entity === 'stremio' ? 'Stream response timeout (seconds)' : 'Response timeout (seconds)',
      ],
    ])
    add({ route, group: `${group} / Sorting`, entity, reveal: ['Sorting'] }, [
      [
        `${entity}.sorting`,
        'Sort by',
        entity === 'stremio'
          ? 'cached language size publication age name source ascending descending priority'
          : 'cached size publication age name source ascending descending priority',
      ],
      [`${entity}.sorting-secondary`, 'Then by (2)'],
      [`${entity}.sorting-third`, 'Then by (3)'],
    ])
  }
  if (entity === 'stremio')
    add({ route, group: `${group} / Filters`, entity, reveal: ['Filters'] }, [
      ['stremio.language-filter', 'Filter languages'],
      ['stremio.language', 'Preferred language'],
      ['stremio.include', 'Include'],
    ])
  for (const [key, title, control] of [
    ['expires', 'Expires at', 'share-expires'],
    ['rate', 'Requests per minute', 'share-request-limit'],
    [
      'concurrent',
      entity === 'share' ? 'Concurrent downloads' : 'Concurrent requests',
      'share-concurrent-limit',
    ],
  ])
    settings.push({
      id: `${entity}.${key}`,
      title,
      route,
      group: `${group} / Advanced`,
      entity,
      control,
      reveal: ['Advanced', key === 'expires' ? 'Expiration' : 'Request limits'],
      keywords: 'expiration expiry rate limits expiration limites',
    })
}

// Source configuration is indexed by field labels, independent of source names,
// credentials, imported records and adapter-specific default values.
function source(
  stage: string,
  fields: readonly (readonly [string, string, string?])[],
  adapters?: string[],
) {
  add(
    {
      route: '/providers/settings',
      group: `Sources / Sources / ${stage === 'collection' ? 'Imports' : stage.charAt(0).toUpperCase() + stage.slice(1)}`,
      entity: 'source',
      stage,
      adapters,
    },
    fields.map(([id, title, keywords]) => [`source.${id}`, title, keywords]),
  )
}
source('identity', [
  ['name', 'Display name'],
  ['id', 'Source identifier'],
  ['adapter', 'Source type', 'adapter torznab HTML JSON'],
  ['enabled', 'Source enabled'],
  ['version', 'Definition version'],
])
source('connection', [
  ['url', 'Source URL'],
  ['auth', 'Authentication type'],
  ['secret', 'Secret reference'],
  ['username', 'Username reference'],
  ['password', 'Password reference'],
  ['placement', 'Credential placement'],
  ['parameter', 'Credential parameter name'],
  ['headers', 'HTTP headers'],
  ['secret-headers', 'Secret header references'],
  ['query', 'Search query'],
  ['categories', 'Categories'],
  ['method', 'HTTP method'],
  ['base-query', 'Base query · Full and all modes'],
  ['incremental-query', 'Incremental query overrides'],
  ['body', 'Request body · JSON'],
  ['listing-paths', 'Additional listing paths'],
  ['search-mode', 'Search mode'],
  ['category-requests', 'Category requests'],
  ['catalog', 'Normalized catalog'],
])
source('pagination', [
  ['pagination', 'Pagination type'],
  ['start', 'Starting page or offset'],
  ['size', 'Page size'],
  ['page-param', 'Page parameter'],
  ['offset-param', 'Offset parameter'],
  ['size-param', 'Page-size parameter'],
  ['cursor-param', 'Cursor parameter'],
  ['location', 'Parameter location'],
  ['next', 'Next page / cursor pointer'],
  ['total', 'Total pointer'],
  ['current', 'Current page pointer'],
])
source(
  'pagination',
  [
    ['next-selector', 'Next-page link selector'],
    ['empty', 'Empty-result selector'],
  ],
  ['html'],
)
source(
  'mapping',
  [
    ['native-from', 'Native ID from'],
    ['native-pattern', 'Native ID pattern'],
  ],
  ['torznab'],
)
source(
  'mapping',
  [
    ['items', 'Items pointer'],
    ['native-id', 'Native source ID pointer'],
    ['fields', 'Field mappings'],
  ],
  ['json', 'http_json'],
)
source(
  'mapping',
  [
    ['container', 'Listing container selector'],
    ['record', 'Record selector'],
    ['native-selector', 'Native ID selector'],
    ['detail-container', 'Detail container selector'],
  ],
  ['html'],
)
source(
  'mapping',
  [
    [
      'html-fields',
      'Field mappings',
      'HTML title info hash size publication date categories IMDb TMDB TVDB season episode languages selector attribute format date layout timezone extraction pattern required multiple values',
    ],
    [
      'html-details',
      'Detail pages',
      'HTML detail link selector attribute format required additional fields',
    ],
  ],
  ['html'],
)
source(
  'pagination',
  [
    [
      'html-checks',
      'Response checks',
      'HTML total records current position selector attribute format required',
    ],
    ['html-request', 'Request parameters', 'HTML form query parameter location page size'],
  ],
  ['html'],
)
source('mapping', [['output', 'Output fields', 'published fields output data']])
source('collection', [
  ['request-interval', 'Request interval', 'pacing frequence cadence'],
  ['timeout', 'Request timeout', 'delay delai'],
  ['rate-reset', 'Rate-limit reset interpretation'],
  ['options', 'Options'],
  ['minute', 'Requests per minute'],
  ['hour', 'Requests per hour'],
  ['day', 'Requests per day'],
])
source(
  'collection',
  [
    ['window', 'Window pages'],
    ['minimum', 'Minimum expected total'],
    ['coverage', 'Coverage total mode', 'at least exact advisory coverage pagination'],
    ['order', 'Incremental listing order'],
    ['totals', 'Total pointers'],
    ['enrich', 'Source detail fields'],
    ['detail-updates', 'Fetch source details after updates'],
    ['scopes', 'Scopes'],
    ['partitions', 'Partitions'],
    ['variants', 'Query variants'],
    ['options-url', 'Options URL'],
    ['groups', 'Groups pointer'],
    ['values', 'Values pointer'],
    ['value', 'Value pointer'],
    ['query-param', 'Query parameter'],
    ['priority', 'Priority pointer'],
    ['discovery', 'Discovery query'],
    ['first-id', 'First native ID'],
    ['resolve-url', 'Resolve URL'],
    ['resolve-pattern', 'Resolve path pattern'],
    ['detail-url', 'Detail URL'],
    ['detail-pointer', 'Detail record pointer'],
    ['detail-native', 'Detail native ID pointer'],
    ['detail-fields', 'Detail field mappings'],
  ],
  ['json', 'http_json'],
)
source('schedules', [
  ['schedule-enabled', 'Schedule enabled'],
  ['timing', 'Timing', 'interval daily time cron expression schedule frequence horaire'],
  ['timezone', 'Timezone', 'schedule timezone fuseau horaire'],
  ['interval', 'Interval'],
  ['daily', 'Daily time'],
  ['cron', 'Cron expression'],
  ['mode', 'Collection mode'],
  ['rebuild', 'Rebuild whole source periodically'],
  ['budget', 'Page budget per tick'],
  ['known-pages', 'Known-page boundary'],
])

for (const setting of settings.filter((entry) => entry.entity === 'source')) {
  if (setting.stage === 'schedules') setting.fallback = 'Automatic imports'
  if (setting.stage === 'pagination') setting.fallback = 'Page strategy'
  if (
    [
      'source.query',
      'source.categories',
      'source.next',
      'source.total',
      'source.current',
      'source.cursor-param',
      'source.body',
    ].includes(setting.id)
  )
    setting.adapters = ['json', 'http_json', 'torznab']
  if (['source.search-mode', 'source.category-requests'].includes(setting.id))
    setting.adapters = ['torznab']
  if (setting.stage === 'mapping') setting.fallback = 'Record selection'
  if (setting.id === 'source.detail-container') setting.fallback = 'Detail pages'
  if (setting.stage === 'connection') setting.fallback = 'Endpoint'
  if (setting.stage === 'collection') setting.fallback = 'Bounded traversal'
  if (
    ['options-url', 'groups', 'values', 'value', 'query-param', 'priority'].includes(
      setting.id.replace('source.', ''),
    )
  )
    setting.fallback = 'Discovered filter options'
  if (
    [
      'discovery',
      'first-id',
      'resolve-url',
      'resolve-pattern',
      'detail-url',
      'detail-pointer',
      'detail-native',
      'detail-fields',
    ].includes(setting.id.replace('source.', ''))
  )
    setting.fallback = 'ID recovery & detail mapping'
}

// Public destinations are a separate allowlist. No administrator labels or source
// names are exposed by the public index, including on an administrator's /public view.
for (const [area, route] of [
  ['stremio', '/public'],
  ['torznab', '/public/torznab'],
  ['search', '/public/search'],
] as const) {
  if (area !== 'torznab')
    add(
      {
        route,
        group:
          area === 'search'
            ? 'Search'
            : `Connections / ${area === 'stremio' ? 'Stremio' : 'Torznab'}`,
        public: area,
        reveal: area === 'search' ? ['Debrid services'] : [],
      },
      [
        ...debridProviders.map(
          (p) => [`visitor.${area}.${providerSettingSuffix(p.id)}`, `${p.name} API key`] as const,
        ),
      ],
    )
  settings.push({
    id: `visitor.${area}.announces`,
    title: 'Private source announce URLs',
    group:
      area === 'search'
        ? 'Search / Advanced'
        : `Connections / ${area === 'stremio' ? 'Stremio' : 'Torznab'} / Advanced`,
    route,
    control: 'public-announce-settings',
    reveal: area === 'search' ? ['Debrid services', 'Advanced'] : ['Advanced'],
    public: area,
    keywords: 'private tracker passkey annonce sources',
  })
  if (area !== 'search')
    add(
      {
        route,
        group: `Connections / ${area === 'stremio' ? 'Stremio' : 'Torznab'} / Filters`,
        public: area,
        reveal: ['Filters'],
      },
      [
        [`visitor.${area}.media`, 'Media types'],
        [`visitor.${area}.sources`, 'Sources'],
        [`visitor.${area}.cached`, 'Cached results only'],
        [`visitor.${area}.services`, 'Services'],
        [`visitor.${area}.max-results`, 'Maximum results'],
        [`visitor.${area}.max-cached`, 'Maximum cached results'],
        [
          `visitor.${area}.response`,
          area === 'stremio' ? 'Stream response timeout (seconds)' : 'Response timeout (seconds)',
        ],
        ...(area === 'stremio'
          ? ([
              [`visitor.${area}.language-filter`, 'Filter languages'],
              [`visitor.${area}.language`, 'Preferred language'],
              [`visitor.${area}.include`, 'Include'],
            ] as const)
          : []),
      ],
    )
  if (area !== 'search')
    add(
      {
        route,
        group: `Connections / ${area === 'stremio' ? 'Stremio' : 'Torznab'} / Sorting`,
        public: area,
        reveal: ['Sorting'],
      },
      [
        [
          `visitor.${area}.sorting`,
          'Sort by',
          area === 'stremio'
            ? 'cached language size publication age name ascending descending priority'
            : 'cached size publication age name ascending descending priority',
        ],
        [`visitor.${area}.sorting-secondary`, 'Then by (2)'],
        [`visitor.${area}.sorting-third`, 'Then by (3)'],
      ],
    )
}

for (const setting of settings) {
  if (setting.id === 'imports.progress' || setting.id === 'imports.recovery') {
    const category = setting.id === 'imports.recovery' ? 'Recovery' : 'Useful progress'
    setting.reveal = ['Advanced', 'Import execution', category]
    setting.group += ` / ${category}`
  }
}

export const settingsCatalog: readonly Setting[] = settings
const byID = new Map(settings.map((setting) => [setting.id, setting]))
export function settingByID(id: string) {
  return byID.get(id)
}
export function settingsHash(id: string) {
  return `#setting=${encodeURIComponent(id)}`
}
export function settingFromHash(hash: string) {
  if (!hash.startsWith('#setting=')) return undefined
  try {
    return settingByID(decodeURIComponent(hash.slice(9)))
  } catch {
    return undefined
  }
}
export function settingURL(setting: Setting, entity?: string) {
  if (setting.entity === 'source' && entity === 'new') return '/providers/new'
  let route = setting.route
  if (setting.entity === 'source' && entity) route = `/providers/${encodeURIComponent(entity)}`
  const url = new URL(route || '/', 'https://settings.invalid')
  if (setting.entity && entity) url.searchParams.set('configure', entity)
  return url.pathname + url.search + settingsHash(setting.id)
}

export type SettingsVisibility = {
  admin: boolean
  search?: boolean
  stremio?: boolean
  torznab?: boolean
  sources?: boolean
  announces?: boolean
  services?: readonly string[]
}
export function visibleSettings(visibility: SettingsVisibility) {
  return settingsCatalog.filter((setting) => {
    if (setting.public === 'interface') return true
    if (visibility.admin) return !setting.public
    if (!setting.public || !visibility[setting.public]) return false
    if (setting.title === 'Sources' && !visibility.sources) return false
    if (setting.id.endsWith('.announces') && !visibility.announces) return false
    const provider = debridProviders.find(
      (provider) => setting.title === `${provider.name} API key`,
    )
    if (provider && !visibility.services?.includes(provider.id)) return false
    return true
  })
}

export function normalizeSettingQuery(value: string) {
  return value
    .normalize('NFKD')
    .replace(/\p{M}/gu, '')
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, ' ')
    .trim()
}
type SearchEntry = {
  setting: Setting
  title: string
  group: string
  terms: string
  words: string[]
}
export function createSettingsIndex(entries: readonly Setting[]) {
  return entries.map((setting): SearchEntry => {
    const title = normalizeSettingQuery(setting.title)
    const group = normalizeSettingQuery(setting.group)
    const terms = normalizeSettingQuery(`${setting.id} ${setting.keywords ?? ''}`)
    return {
      setting,
      title,
      group,
      terms,
      words: [...new Set(`${title} ${group} ${terms}`.split(' '))],
    }
  })
}
function oneEdit(a: string, b: string) {
  if (Math.abs(a.length - b.length) > 1) return false
  let left = 0,
    right = 0,
    edits = 0
  while (left < a.length && right < b.length) {
    if (a[left] === b[right]) {
      left++
      right++
      continue
    }
    if (++edits > 1) return false
    if (a.length >= b.length) left++
    if (b.length >= a.length) right++
  }
  return edits + (left < a.length || right < b.length ? 1 : 0) <= 1
}
export function searchSettings(index: ReturnType<typeof createSettingsIndex>, query: string) {
  const normalized = normalizeSettingQuery(query).slice(0, 160)
  const tokens = [...new Set(normalized.split(' ').filter(Boolean))].slice(0, 12)
  if (!tokens.length) return []
  return index
    .flatMap((entry) => {
      let score = entry.title === normalized ? 1000 : entry.title.includes(normalized) ? 300 : 0
      for (const token of tokens) {
        if (entry.title.split(' ').includes(token)) score += 50
        else if (entry.title.includes(token)) score += 30
        else if (entry.group.includes(token)) score += 15
        else if (entry.terms.includes(token)) score += 5
        else if (
          token.length >= 5 &&
          entry.words.some((word) => word.length >= 5 && oneEdit(token, word))
        )
          score += 1
        else return []
      }
      return [{ setting: entry.setting, score }]
    })
    .sort(
      (a, b) =>
        b.score - a.score ||
        a.setting.group.localeCompare(b.setting.group) ||
        a.setting.title.localeCompare(b.setting.title),
    )
    .map(({ setting }) => setting)
}
