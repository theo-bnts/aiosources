import { Link } from 'react-router-dom'
import { number } from '@/lib/format'
import { cn } from '@/lib/utils'

export function Statistic({
  label,
  value,
  to,
  size = 'default',
}: {
  label: string
  value: number | string
  to?: string
  size?: 'default' | 'sm'
}) {
  const text = typeof value === 'number' ? number(value) : value
  return (
    <div className="statistic-card">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd
        className={cn(
          'mt-2 [overflow-wrap:anywhere]',
          size === 'sm' ? 'text-sm' : 'text-xl font-semibold tabular-nums',
        )}
      >
        {to ? (
          <Link className="data-link" to={to} aria-label={`View ${label.toLowerCase()}`}>
            {text}
          </Link>
        ) : (
          text
        )}
      </dd>
    </div>
  )
}
