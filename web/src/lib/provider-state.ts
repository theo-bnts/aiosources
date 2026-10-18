import { api, ApiError } from './api'
import { parseProviderJSON } from './provider-json'
import type { ProviderDocument } from './types'

export async function toggleProvider(source: { id: string; revision: string; enabled: boolean }) {
  const document = await api<ProviderDocument>(`/providers/${encodeURIComponent(source.id)}`)
  if (document.revision !== source.revision)
    throw new ApiError(409, 'This source was changed elsewhere. Refresh before changing its state.')
  const json = parseProviderJSON(document.json)
  if (json.errors.length) throw new Error('Fix the JSON before changing its enabled state.')
  json.set('enabled', !source.enabled)
  return api<ProviderDocument>(`/providers/${encodeURIComponent(source.id)}`, {
    method: 'PUT',
    body: JSON.stringify({ json: json.toString(), revision: source.revision }),
  })
}
