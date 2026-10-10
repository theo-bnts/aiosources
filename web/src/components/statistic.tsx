import { Link } from 'react-router-dom'
import { number } from '@/lib/format'

export function Statistic({
  label,
  value,
  to,
}: {
  label: string
  value: number | string
  to?: string
}) {
  const text = typeof value === 'number' ? number(value) : value
  return (
    <div className="statistic-card">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-2 text-xl font-semibold tabular-nums">
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
