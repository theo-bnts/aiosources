import { Link } from 'react-router-dom'
import type { Run } from '@/lib/types'
import { messageText, useDate } from '@/lib/format'
import { RunStatusBadge } from '@/components/common'

export function ImportLastRun({
  run,
  remote = false,
  next,
  state,
  hint,
}: {
  run?: Run
  remote?: boolean
  next?: string
  state?: unknown
  hint?: string
}) {
  const date = useDate()
  const error = run && ['failed', 'paused'].includes(run.status) ? run.error : undefined
  return (
    <div>
      <dt className="text-muted-foreground">Last run</dt>
      <dd className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-2">
        {run ? (
          <Link
            to={`${remote ? '/remotes/history' : '/runs'}/${encodeURIComponent(run.id)}`}
            state={state}
          >
            <RunStatusBadge run={run} />
          </Link>
        ) : (
          <span className="text-muted-foreground">No runs</span>
        )}
        {next && <span className="text-muted-foreground">Next: {date(next)}</span>}
      </dd>
      {error && (
        <p
          className={`help mt-1 break-words ${run?.status === 'failed' ? 'text-destructive' : 'text-amber-800 dark:text-amber-300'}`}
        >
          {messageText(error)}
        </p>
      )}
      {hint && <p className="help mt-1">{hint}</p>}
    </div>
  )
}
