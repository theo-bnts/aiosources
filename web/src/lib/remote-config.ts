import type { ProviderDocument } from './types'
import { parseProviderJSON } from './provider-json'
import { isJSONAdapter } from './source-adapters'
import { catalogEntityKeys, type CatalogEntities } from './catalog-types'

export interface RemoteDraft extends Required<CatalogEntities> {
  id: string
  name: string
  url: string
  enabled: boolean
  secretRef: string
  autoSync: boolean
  intervalMinutes: string
}

export function remoteDraft(document?: ProviderDocument): RemoteDraft {
  const provider = document?.provider
  return {
    id: provider?.id ?? '',
    name: provider?.name ?? '',
    url: provider?.url ?? '',
    enabled: provider?.enabled ?? false,
    secretRef: provider?.auth.secret_ref ?? '',
    metadata: provider?.catalog_entities?.metadata ?? false,
    availability: provider?.catalog_entities?.availability ?? false,
    identifiers: provider?.catalog_entities?.identifiers ?? false,
    languages: provider?.catalog_entities?.languages ?? false,
    technical: provider?.catalog_entities?.technical ?? false,
    files: provider?.catalog_entities?.files ?? false,
    autoSync: provider?.catalog_sync?.enabled ?? !document,
    intervalMinutes: String(provider?.catalog_sync?.interval_minutes ?? 60),
  }
}

export function validateRemoteURL(value: string) {
  let url: URL
  try {
    url = new URL(value)
  } catch {
    throw new Error('Enter a complete HTTPS catalog URL.')
  }
  if (
    url.protocol !== 'https:' ||
    !url.hostname ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    /[?#]/.test(value)
  ) {
    throw new Error(
      'Use an HTTPS catalog URL without a username, password, query string or fragment. Enter the sharing password separately.',
    )
  }
}

export function remoteJSON(
  document: ProviderDocument | undefined,
  draft: RemoteDraft,
  secretRef: string,
) {
  validateRemoteURL(draft.url.trim())
  if (
    !/^[0-9]+$/.test(draft.intervalMinutes) ||
    Number(draft.intervalMinutes) < 1 ||
    Number(draft.intervalMinutes) > 525600
  )
    throw new Error('Enter a synchronization interval between 1 and 525600 whole minutes.')
  const json = parseProviderJSON(
    document?.json ??
      `{
  "version": 1,
  "adapter": "json",
  "request_interval": "1s",
  "rate_limit_reset": "epoch",
  "page_size": 100,
  "http": {
    "method": "GET",
    "items_path": "/items",
    "catalog": true
  },
  "pagination": {
    "type": "cursor",
    "in": "query",
    "cursor_param": "cursor",
    "size_param": "limit",
    "next_path": "/next_cursor",
    "start": 0
  },
  "mapping": {
    "id": "/id",
    "fields": {}
  },
  "output": {
    "fields": ["title", "size", "info_hash", "published_at", "category", "categories"]
  }
}
`,
  )
  if (json.errors.length)
    throw new Error('Fix the source JSON before changing its remote settings.')
  if (document && (!isJSONAdapter(document.provider.adapter) || !document.provider.http.catalog)) {
    throw new Error(
      'This source is no longer a remote catalog. Reload its configuration in Sources.',
    )
  }
  const initial = remoteDraft(document)
  const update = (path: string[], value: unknown, previous: unknown) => {
    if (document && value === previous) return
    if (value === undefined) json.deleteIn(path)
    else json.setIn(path, value)
  }
  if (!document) json.set('id', draft.id.trim())
  update(['name'], draft.name.trim(), initial.name)
  update(['url'], draft.url.trim(), initial.url)
  update(['enabled'], draft.enabled, initial.enabled)
  if (catalogEntityKeys.some((key) => draft[key]))
    json.set(
      'catalog_entities',
      Object.fromEntries(catalogEntityKeys.filter((key) => draft[key]).map((key) => [key, true])),
    )
  else json.delete('catalog_entities')
  json.delete('schedule')
  json.set('catalog_sync', {
    enabled: draft.autoSync,
    interval_minutes: Number(draft.intervalMinutes),
  })
  if (!document || secretRef !== initial.secretRef) {
    json.setIn(['auth', 'type'], 'bearer')
    json.setIn(['auth', 'secret_ref'], secretRef)
    for (const key of ['username_ref', 'password_ref', 'in', 'name']) json.deleteIn(['auth', key])
  }
  return json.toString()
}

export function mergeRemoteDraft(
  current: RemoteDraft,
  previous: RemoteDraft,
  latest: RemoteDraft,
): RemoteDraft {
  return {
    id: latest.id,
    name: current.name === previous.name ? latest.name : current.name,
    url: current.url === previous.url ? latest.url : current.url,
    enabled: current.enabled === previous.enabled ? latest.enabled : current.enabled,
    secretRef: current.secretRef === previous.secretRef ? latest.secretRef : current.secretRef,
    metadata: current.metadata === previous.metadata ? latest.metadata : current.metadata,
    availability:
      current.availability === previous.availability ? latest.availability : current.availability,
    identifiers:
      current.identifiers === previous.identifiers ? latest.identifiers : current.identifiers,
    languages: current.languages === previous.languages ? latest.languages : current.languages,
    technical: current.technical === previous.technical ? latest.technical : current.technical,
    files: current.files === previous.files ? latest.files : current.files,
    autoSync: current.autoSync === previous.autoSync ? latest.autoSync : current.autoSync,
    intervalMinutes:
      current.intervalMinutes === previous.intervalMinutes
        ? latest.intervalMinutes
        : current.intervalMinutes,
  }
}
