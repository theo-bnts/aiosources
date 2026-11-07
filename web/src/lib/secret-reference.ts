import { randomID } from './random-id.ts'

// Keep the service and credential purpose readable without changing references
// already used by saved configurations. The random suffix prevents collisions.
export function newSecretReference(prefix: string) {
  const name = prefix
    .normalize('NFKD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
    .slice(0, 95)
    .replace(/_+$/g, '')
  return `${name || 'secret'}_${randomID()}`
}

export function sourceCredentialPurpose(field: string, type: string, parameter: string) {
  if (field === 'username_ref') return 'username'
  if (field === 'password_ref') return 'password'
  if (/pass[\s_-]?key/i.test(parameter)) return 'passkey'
  if (type === 'cookie') return 'cookie'
  if (/token/i.test(parameter)) return 'token'
  return ['bearer', 'api_key', 'header', 'query'].includes(type) ? 'api_key' : 'credential'
}
