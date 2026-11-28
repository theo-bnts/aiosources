import {
  catalogCardSummary,
  catalogCategories,
  type CatalogCategoryID,
  type CatalogCategorySpec,
} from './catalog-cards.ts'

export type NotificationChannelType =
  'discord' | 'ntfy' | 'gotify' | 'telegram' | 'slack' | 'webhook'

export type NotificationChannelConfig = {
  username?: string
  server_url?: string
  topic?: string
  priority?: string
  chat_id?: string
  thread_id?: string
  header_name?: string
}

export type NotificationChannel = {
  id: string
  name: string
  type: NotificationChannelType
  enabled: boolean
  is_default: boolean
  allow_trusted: boolean
  config: NotificationChannelConfig
  secret_ref?: string
  header_secret_ref?: string
  active_alerts?: number
  revision: number
  created_at: string
  updated_at: string
}

export type NotificationChannelSummary = {
  id: string
  name: string
  type: NotificationChannelType
  is_default: boolean
}

export type ReleaseAlertFilters = {
  providers?: string[]
  language?: string
  language_mode?: '' | 'all' | 'preferred' | 'multi' | 'unknown'
  min_size?: number
  max_size?: number
  cached_only?: boolean
}

export type ReleaseAlert = {
  id: string
  owner_type: 'admin' | 'trusted'
  owner_id?: string
  owner_email?: string
  work_id: number
  media_type: 'movie' | 'tv' | 'game' | 'software' | 'music' | 'book'
  title: string
  cover?: string
  years?: number[]
  season?: number
  episode?: number
  channel_id: string
  channel_name?: string
  channel_type?: NotificationChannelType
  filters: ReleaseAlertFilters
  status: 'watching' | 'retrying'
  attempts: number
  next_attempt: string
  last_error?: string
  revision: number
  created_at: string
  updated_at: string
}

export const notificationChannelTypes: readonly {
  value: NotificationChannelType
  label: string
  description: string
}[] = [
  {
    value: 'discord',
    label: 'Discord',
    description: 'Send rich embeds with covers and direct links to a Discord channel webhook.',
  },
  {
    value: 'ntfy',
    label: 'ntfy',
    description: 'Push notifications to ntfy.sh or your self-hosted ntfy server.',
  },
  {
    value: 'gotify',
    label: 'Gotify',
    description: 'Send push notifications to a self-hosted Gotify server.',
  },
  {
    value: 'telegram',
    label: 'Telegram',
    description: 'Deliver messages via the Telegram Bot HTTP API without running a bot.',
  },
  {
    value: 'slack',
    label: 'Slack / Mattermost',
    description: 'Post formatted messages to a Slack or Mattermost incoming webhook.',
  },
  {
    value: 'webhook',
    label: 'Generic webhook',
    description: 'Send structured JSON payloads to any HTTP endpoint (n8n, Home Assistant, etc.).',
  },
]

export const ntfyPriorities = [
  { value: '', label: 'Default' },
  { value: 'min', label: 'Min' },
  { value: 'low', label: 'Low' },
  { value: 'default', label: 'Normal' },
  { value: 'high', label: 'High' },
  { value: 'urgent', label: 'Urgent' },
] as const

export function notificationChannelTypeLabel(type?: string): string {
  return notificationChannelTypes.find((item) => item.value === type)?.label ?? type ?? ''
}

export function formatAlertEpisode(season?: number, episode?: number): string {
  if (season === undefined || episode === undefined) return ''
  return `S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}`
}

export function formatAlertByteSize(bytes?: number): string {
  if (!bytes || bytes <= 0) return ''
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000
    unit++
  }
  const rounded = value >= 100 || unit === 0 ? Math.round(value) : Number(value.toFixed(2))
  return `${rounded} ${units[unit]}`
}

export function formatAlertSizeInput(bytes?: number): string {
  if (!bytes || bytes <= 0) return ''
  const binaryUnits: [number, string][] = [
    [1024 ** 4, 'TiB'],
    [1024 ** 3, 'GiB'],
    [1024 ** 2, 'MiB'],
    [1024, 'KiB'],
  ]
  for (const [factor, suffix] of binaryUnits) {
    if (bytes >= factor && bytes % factor === 0) {
      return `${bytes / factor} ${suffix}`
    }
  }
  const decimalUnits: [number, string][] = [
    [1000 ** 4, 'TB'],
    [1000 ** 3, 'GB'],
    [1000 ** 2, 'MB'],
    [1000, 'KB'],
  ]
  for (const [factor, suffix] of decimalUnits) {
    if (bytes >= factor && bytes % factor === 0) {
      return `${bytes / factor} ${suffix}`
    }
  }
  return formatAlertByteSize(bytes)
}

const alertSizeRE = /^([0-9]+(?:[.,][0-9]+)?)\s*(B|O|bytes|octets|[KMGTPE](?:i?[BO])?)$/i
const plainIntegerRE = /^[0-9]+$/

export function parseAlertSizeInput(raw: string): { bytes?: number; error?: string } {
  const trimmed = raw.trim()
  if (!trimmed) return {}
  if (plainIntegerRE.test(trimmed)) {
    const n = Number(trimmed)
    if (!Number.isSafeInteger(n) || n < 0) return { error: 'Size exceeds the supported range.' }
    return n === 0 ? {} : { bytes: n }
  }
  const match = alertSizeRE.exec(trimmed)
  if (!match) return { error: 'Enter bytes or a size such as 1 GB or 500 MB.' }
  const amount = Number(match[1].replace(',', '.'))
  if (!Number.isFinite(amount) || amount < 0) return { error: 'Enter a valid nonnegative size.' }
  let unit = match[2].toUpperCase()
  if (unit === 'BYTES' || unit === 'OCTETS') unit = 'B'
  unit = unit.replace('O', 'B')
  let factor = 1
  if (unit !== 'B') {
    const power = 'KMGTPE'.indexOf(unit[0]) + 1
    const base = unit.includes('I') || unit.length === 1 ? 1024 : 1000
    factor = base ** power
  }
  const bytes = Math.round(amount * factor)
  if (!Number.isSafeInteger(bytes) || bytes < 0) {
    return { error: 'Size exceeds the supported range.' }
  }
  return bytes === 0 ? {} : { bytes }
}

export function hasActiveAlertFilters(filters?: ReleaseAlertFilters): boolean {
  if (!filters) return false
  return Boolean(
    filters.providers?.length ||
    (filters.language_mode && filters.language_mode !== 'all') ||
    (filters.min_size && filters.min_size > 0) ||
    (filters.max_size && filters.max_size > 0) ||
    filters.cached_only,
  )
}

export function formatAlertFiltersSummary(
  filters: ReleaseAlertFilters | undefined,
  sourceName: (id: string) => string = (id) => id,
): string[] {
  if (!filters) return []
  const parts: string[] = []
  if (filters.providers?.length) {
    parts.push(filters.providers.map((id) => sourceName(id)).join(', '))
  }
  if (filters.language_mode && filters.language_mode !== 'all' && filters.language) {
    const lang = filters.language.toUpperCase()
    const suffix =
      filters.language_mode === 'preferred'
        ? ''
        : filters.language_mode === 'multi'
          ? ' + MULTI'
          : ' + MULTI + unknown'
    parts.push(`${lang}${suffix}`)
  }
  if (filters.min_size && filters.max_size) {
    parts.push(
      `${formatAlertByteSize(filters.min_size)} – ${formatAlertByteSize(filters.max_size)}`,
    )
  } else if (filters.min_size) {
    parts.push(`Min ${formatAlertByteSize(filters.min_size)}`)
  } else if (filters.max_size) {
    parts.push(`Max ${formatAlertByteSize(filters.max_size)}`)
  }
  if (filters.cached_only) {
    parts.push('Cached only')
  }
  return parts
}

export function alertCategoryID(mediaType: ReleaseAlert['media_type']): CatalogCategoryID {
  switch (mediaType) {
    case 'movie':
    case 'tv':
      return 'videos'
    case 'game':
      return 'games'
    case 'software':
      return 'software'
    case 'music':
      return 'music'
    case 'book':
      return 'books'
  }
}

export type AlertCategorySection = {
  category: CatalogCategorySpec
  items: ReleaseAlert[]
}

// Group active alerts in the exact Search category order and frames, omitting
// any category with no active alerts so disabled or empty catalogs never clutter
// the view.
export function groupAlertsByCategory(alerts: readonly ReleaseAlert[]): AlertCategorySection[] {
  const buckets = new Map<CatalogCategoryID, ReleaseAlert[]>()
  for (const alert of alerts) {
    const id = alertCategoryID(alert.media_type)
    const list = buckets.get(id)
    if (list) list.push(alert)
    else buckets.set(id, [alert])
  }
  return catalogCategories.flatMap((category) => {
    const items = buckets.get(category.id)
    return items?.length ? [{ category, items }] : []
  })
}

export function alertCardSummary(alert: ReleaseAlert): string {
  if (alert.media_type === 'tv') {
    const episode = formatAlertEpisode(alert.season, alert.episode)
    if (episode) return `Series · ${episode}`
  }
  return catalogCardSummary({ type: alert.media_type, years: alert.years })
}

export function alertCardMetric(
  alert: ReleaseAlert,
  sourceName: (id: string) => string = (id) => id,
  showOwner = false,
): { label: string; title: string } {
  const filterParts = formatAlertFiltersSummary(alert.filters, sourceName)
  const channelLabel = alert.channel_name || notificationChannelTypeLabel(alert.channel_type)
  const channelDetail =
    alert.channel_name && alert.channel_type
      ? `${alert.channel_name} (${notificationChannelTypeLabel(alert.channel_type)})`
      : channelLabel
  const ownerPart =
    showOwner && alert.owner_type === 'trusted' && alert.owner_email ? alert.owner_email : ''

  if (alert.status === 'retrying') {
    const details = [
      'Retrying delivery',
      alert.last_error,
      channelDetail,
      filterParts.join(' · '),
      ownerPart,
    ].filter(Boolean)
    return {
      label: 'Retrying delivery',
      title: details.join(' · '),
    }
  }

  const compactParts = [channelLabel, ...filterParts].filter(Boolean)
  const fullParts = [
    channelDetail,
    filterParts.length > 0 ? filterParts.join(' · ') : 'Any matching release',
    ownerPart,
  ].filter(Boolean)
  return {
    label: compactParts.join(' · ') || 'Watching',
    title: fullParts.join(' · '),
  }
}
