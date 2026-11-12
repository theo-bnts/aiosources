import { Switch } from './ui/switch'
import { Label } from './ui/label'

export type CredentialPolicy = { use_instance_debrid: boolean; use_instance_announces: boolean }
export function CredentialPolicyControls({
  value,
  onChange,
  disabled = false,
  id,
}: {
  value: CredentialPolicy
  onChange: (value: Partial<CredentialPolicy>) => void
  disabled?: boolean
  id: string
}) {
  return (
    <>
      <div className="flex items-start gap-3">
        <Switch
          id={`${id}-debrid`}
          checked={value.use_instance_debrid}
          disabled={disabled}
          onCheckedChange={(use_instance_debrid) => onChange({ use_instance_debrid })}
        />
        <Label
          htmlFor={`${id}-debrid`}
          help="When enabled, playback and availability checks use the instance’s debrid accounts. API keys remain on the server. When disabled, users provide their own keys, remembered only in their encrypted browser configuration. Provider account and connection restrictions still apply."
        >
          Use instance debrid accounts
        </Label>
      </div>
      <div className="flex items-start gap-3">
        <Switch
          id={`${id}-announces`}
          checked={value.use_instance_announces}
          disabled={disabled}
          onCheckedChange={(use_instance_announces) => onChange({ use_instance_announces })}
        />
        <Label
          htmlFor={`${id}-announces`}
          help="When enabled, uncached private-source downloads can use the instance’s announce URLs. Torznab clients receive these URLs in magnets: enable only for recipients you authorize to receive them. When disabled, personal announce URLs are required for uncached private sources. Cached-only results never include an announce URL."
        >
          Use instance announce URLs
        </Label>
      </div>
    </>
  )
}
