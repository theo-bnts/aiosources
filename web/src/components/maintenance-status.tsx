import { Link } from 'react-router-dom'
import { ErrorNotice } from '@/components/common'
import type { Status } from '@/lib/aiosource-types'

const labels = {
  books: 'Book information',
  videos: 'Video information',
  metadata: 'Video and book information',
  mapping: 'Automatic identifications',
  cache: 'Cache observations',
}

export function MaintenanceStatus({
  value,
  scope,
}: {
  value: Status['maintenance']
  scope: Exclude<NonNullable<Status['maintenance']>['scope'], ''>
}) {
  if (!value?.scope || !value.status || (value.status !== 'running' && value.scope !== scope))
    return null
  return (
    <div className="space-y-2" aria-label="Maintenance status">
      <p role="status" className="help">
        {labels[value.scope]}{' '}
        {value.status === 'running'
          ? `${value.scope === 'mapping' || value.scope === 'cache' ? 'are' : 'is'} being reset. You can leave this page while the reset completes.`
          : value.status === 'succeeded'
            ? 'reset completed.'
            : 'reset did not complete.'}{' '}
        <Link to="/logs" className="data-link">
          View logs
        </Link>
      </p>
      <ErrorNotice error={value.error} scope="action" />
    </div>
  )
}
