import { Disclosure } from '@/components/ui/disclosure'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { useEffect, useState } from 'react'
import { usePublicCredential } from '@/lib/public-credentials'

// Clear personal inputs without resetting the controlled, non-secret filters.
export function clearPublicCredentials(form: HTMLFormElement) {
  for (const input of form.querySelectorAll<HTMLInputElement>('input[data-sensitive][name]')) {
    input.value = ''
  }
}
export type PublicInfo = {
  stremio_enabled?: boolean
  torznab_enabled?: boolean
  search_enabled?: boolean
  sources?: { id: string; name: string }[]
  enabled: boolean
  providers: { id: string; name: string }[]
  private_sources?: { id: string; name: string }[]
}
export function PublicAnnounceFields({
  sources,
  pending,
  torznab = false,
}: {
  sources: { id: string; name: string }[]
  pending: boolean
  torznab?: boolean
}) {
  const saved = usePublicCredential()
  const [removed, setRemoved] = useState<string[]>([])
  useEffect(() => setRemoved([]), [saved])
  if (!sources.length) return null
  return (
    <Disclosure title="Advanced">
      <p className="help">
        Optional: supply your own complete announce URL to allow uncached downloads from a private
        source.{' '}
        {torznab
          ? 'Your client receives your announce in its magnets.'
          : 'These URLs are sent only to your debrid account when needed.'}{' '}
        They are remembered in your encrypted browser configuration and included in your connection
        links.
      </p>
      {sources.map((source) => {
        const retained = saved?.announces?.includes(source.id) && !removed.includes(source.id)
        return (
          <div className="field" key={source.id}>
            <Label htmlFor={`public-announce-${source.id}`}>{source.name} announce URL</Label>
            <div className="flex min-w-0 flex-col gap-2 sm:flex-row">
              <Input
                id={`public-announce-${source.id}`}
                name={`announce:${source.id}`}
                type="password"
                autoComplete="off"
                spellCheck={false}
                autoCapitalize="off"
                placeholder={
                  retained
                    ? 'Saved on this browser · enter a URL to replace it'
                    : 'Complete announce URL'
                }
                maxLength={8192}
                disabled={pending}
              />
              {retained && (
                <Button
                  type="button"
                  variant="warning"
                  size="sm"
                  className="self-start sm:self-auto"
                  disabled={pending}
                  onClick={() => setRemoved((ids) => [...ids, source.id])}
                >
                  Remove announce
                </Button>
              )}
            </div>
          </div>
        )
      })}
      {removed.map((id) => (
        <input key={id} type="hidden" name="remove_announce" value={id} />
      ))}
      {removed.length > 0 && (
        <p className="help">Apply your changes to remove these saved announces.</p>
      )}
    </Disclosure>
  )
}
