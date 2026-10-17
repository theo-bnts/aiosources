import { Disclosure } from '@/components/ui/disclosure'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
export type PublicInfo = {
  torznab_enabled?: boolean
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
  if (!sources.length) return null
  return (
    <Disclosure title="Advanced">
      <p className="help">
        Optional: supply your own complete announce URL to allow uncached{' '}
        {torznab ? 'downloads' : 'playback'} from a private source.{' '}
        {torznab
          ? 'Your client receives your announce in its magnets.'
          : 'These URLs are sent only to your debrid account when needed.'}{' '}
        They stay in your encrypted connection link.
      </p>
      {sources.map((source) => (
        <div className="field" key={source.id}>
          <Label htmlFor={`public-announce-${source.id}`}>{source.name} announce URL</Label>
          <Input
            id={`public-announce-${source.id}`}
            name={`announce:${source.id}`}
            type="password"
            autoComplete="off"
            spellCheck={false}
            autoCapitalize="off"
            placeholder="Complete announce URL"
            maxLength={8192}
            disabled={pending}
          />
        </div>
      ))}
    </Disclosure>
  )
}
