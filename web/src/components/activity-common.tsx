import { BugIcon, InfoIcon, WarningIcon, WarningCircleIcon } from '@phosphor-icons/react'
import { Link } from 'react-router-dom'
import { activityServices, type ActivityLevel } from '@/lib/activity-types'
import { useSourceNames } from '@/lib/source-names'
import { Badge } from '@/components/ui/badge'

export function LevelBadge({ level }: { level: ActivityLevel }) {
  const Icon = { debug: BugIcon, info: InfoIcon, warn: WarningIcon, error: WarningCircleIcon }[
    level
  ]
  return (
    <Badge
      variant={
        level === 'error'
          ? 'destructive'
          : level === 'warn'
            ? 'warning'
            : level === 'info'
              ? 'info'
              : 'secondary'
      }
    >
      <Icon aria-hidden="true" />
      {level}
    </Badge>
  )
}

export function ActivityLinks({
  provider_id,
  run_id,
  kind,
  service,
}: {
  provider_id?: string
  run_id?: string
  kind?: string
  service?: string
}) {
  const sourceName = useSourceNames()
  const servicePage = activityServices.find((item) => item.id === service)
  const operationsPage = kind?.startsWith('health.')
    ? { to: '/health', label: 'Storage' }
    : undefined
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs">
      {servicePage && (
        <Link className="data-link" to={servicePage.to}>
          {servicePage.name}
        </Link>
      )}
      {provider_id && (
        <Link className="data-link break-all" to={`/providers/${encodeURIComponent(provider_id)}`}>
          Source: {sourceName(provider_id)}
        </Link>
      )}
      {run_id && (
        <Link
          className="data-link font-mono"
          title={run_id}
          to={`/runs/${encodeURIComponent(run_id)}`}
        >
          Run: {run_id.slice(0, 8)}
        </Link>
      )}
      {operationsPage && (
        <Link className="data-link" to={operationsPage.to}>
          {operationsPage.label}
        </Link>
      )}
      {!provider_id && !run_id && !operationsPage && !servicePage && (
        <span className="text-muted-foreground">Service</span>
      )}
    </div>
  )
}
