import type { TrustedPermissions } from '@/lib/trusted-users'
import { trustedPermissionControls } from '@/lib/trusted-users'
import { Switch } from './ui/switch'
import { Label } from './ui/label'

export function AccessPermissions({
  id,
  value,
  onChange,
  disabled,
}: {
  id: string
  value: Pick<
    TrustedPermissions,
    'search_enabled' | 'torznab_enabled' | 'stremio_enabled' | 'catalog_enabled'
  >
  onChange: (value: Partial<TrustedPermissions>) => void
  disabled?: boolean
}) {
  return (
    <div className="space-y-5">
      {trustedPermissionControls.map(([key, label, help]) => (
        <div className="flex items-center gap-3" key={key}>
          <Switch
            id={`${id}-${key.replaceAll('_', '-')}`}
            checked={value[key]}
            disabled={disabled}
            onCheckedChange={(checked) => onChange({ [key]: checked })}
          />
          <Label htmlFor={`${id}-${key.replaceAll('_', '-')}`} help={help}>
            {label}
          </Label>
        </div>
      ))}
    </div>
  )
}
