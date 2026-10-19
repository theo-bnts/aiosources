import { Link } from 'react-router-dom'
import { number } from '@/lib/format'
import { cn } from '@/lib/utils'

export function Statistic({
  label,
  value,
  to,
  size = 'default',
  detail,
  className,
  valueClassName,
  loadingLabel,
}: {
  label: string
  value: number | string
  to?: string
  size?: 'default' | 'sm'
  detail?: string
  className?: string
  valueClassName?: string
  loadingLabel?: string
}) {
  const text = typeof value === 'number' ? number(value) : value
  return (
    <div className={cn('statistic-card', className)}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd
        aria-busy={Boolean(loadingLabel)}
        className={cn(
          'mt-2 [overflow-wrap:anywhere]',
          size === 'sm' ? 'text-sm' : 'text-xl font-semibold tabular-nums',
          valueClassName,
        )}
      >
        {loadingLabel ? (
          <span role="status" className="text-sm font-normal text-muted-foreground">
            {loadingLabel}
          </span>
        ) : to ? (
          <Link
            className={cn('data-link', valueClassName)}
            to={to}
            aria-label={`View ${label.toLowerCase()}`}
          >
            {text}
          </Link>
        ) : (
          text
        )}
        {detail && <span className="ml-2 text-sm font-normal text-muted-foreground">{detail}</span>}
      </dd>
    </div>
  )
}
