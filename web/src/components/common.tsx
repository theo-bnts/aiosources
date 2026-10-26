import {
  createContext,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import {
  ArrowLeftIcon,
  ArrowRightIcon,
  WarningCircleIcon,
  ArrowClockwiseIcon,
  DatabaseIcon,
  MagnifyingGlassIcon,
  XIcon,
  type Icon,
} from '@phosphor-icons/react'
import { Link } from 'react-router-dom'
import { ApiError } from '@/lib/api'
import { RequestNotices } from '@/lib/request-notices'
import { messageText, number, statusLabels } from '@/lib/format'
import { useDetailBackLink, type BackLink } from '@/lib/detail-navigation'
import type { Run } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'

export function PageHeader({
  title,
  description,
  actions,
  back: defaultBack,
}: {
  title: string
  description?: string
  actions?: ReactNode
  back?: BackLink
}) {
  const level = useContext(PageHeadingLevel)
  const visibility = useContext(PageHeaderVisibility)
  const Heading = level === 1 ? 'h1' : 'h2'
  const back = useDetailBackLink(defaultBack)
  const showDescription = visibility.description && description
  if (!visibility.title && !showDescription && !actions && !back) return null
  return (
    <header className="space-y-4">
      {back && (
        <Link
          to={back.to}
          state={back.state}
          className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeftIcon aria-hidden="true" className="size-4" />
          {back.label}
        </Link>
      )}
      <div className="page-heading">
        {(visibility.title || showDescription) && (
          <div className="min-w-0 flex-1">
            {visibility.title && (
              <Heading className={`break-words ${level === 1 ? 'page-title' : 'section-title'}`}>
                {title}
              </Heading>
            )}
            {showDescription && (
              <p
                className={
                  visibility.title
                    ? 'page-description break-words'
                    : 'max-w-3xl break-words text-sm leading-6 text-muted-foreground'
                }
              >
                {description}
              </p>
            )}
          </div>
        )}
        {actions && <div className="toolbar ml-auto">{actions}</div>}
      </div>
    </header>
  )
}

export const PageHeadingLevel = createContext<1 | 2>(1)
export const PageHeaderVisibility = createContext({ title: true, description: true })

export function ErrorNotice({
  error,
  retry,
  retryLabel = 'Try again',
  presentation = 'inline',
  scope = 'form',
}: {
  error: unknown
  retry?: () => void
  retryLabel?: string
  presentation?: 'inline' | 'notification'
  scope?: 'form' | 'action'
}) {
  const register = useContext(RequestNotices)
  const id = useId()
  const notification =
    register &&
    presentation === 'inline' &&
    Boolean(error) &&
    (scope === 'action' ||
      (error instanceof ApiError &&
        (error.status === 0 || error.status === 408 || error.status >= 500)))
  useEffect(() => {
    if (notification) return register(id, { error, retry })
  }, [notification, register, id, error, retry])
  if (!error || notification) return null
  return (
    <div
      role={presentation === 'notification' ? 'presentation' : 'alert'}
      className={`rounded-md border border-destructive/40 bg-card p-4 text-card-foreground shadow-sm ${presentation === 'notification' ? 'pr-12' : ''}`}
    >
      <div className="flex items-start gap-3">
        <WarningCircleIcon size={20} className="mt-0.5 shrink-0 text-destructive" />
        <div className="min-w-0 flex-1 space-y-2">
          <p className="break-words text-sm">
            {error instanceof Error ? messageText(error.message) : 'An unexpected error occurred.'}
          </p>
          {error instanceof ApiError && error.details.length > 0 && (
            <ul className="list-disc space-y-1 pl-4 text-xs text-muted-foreground">
              {error.details.map((issue, i) => (
                <li key={i}>{issue}</li>
              ))}
            </ul>
          )}
          {retry && (
            <Button type="button" size="sm" variant="outline" onClick={retry}>
              <ArrowClockwiseIcon />
              {retryLabel}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

export function Loading({ label = 'Loading data' }: { label?: string }) {
  return (
    <div role="status" aria-label={label} className="space-y-3 py-3">
      <span className="sr-only">{label}</span>
      <Skeleton className="h-9 w-full" />
      <Skeleton className="h-12 w-full" />
      <Skeleton className="h-12 w-full" />
      <Skeleton className="h-12 w-3/4" />
    </div>
  )
}

export function Empty({
  title,
  description,
  action,
  icon: EmptyIcon = DatabaseIcon,
}: {
  title: string
  description: string
  action?: ReactNode
  icon?: Icon
}) {
  return (
    <div className="flex flex-col items-start gap-3 rounded-md border border-dashed border-border px-6 py-12">
      <EmptyIcon aria-hidden="true" className="size-6 text-muted-foreground" />
      <div>
        <h2 className="section-title">{title}</h2>
        <p className="mt-1 max-w-xl text-sm leading-6 text-muted-foreground">{description}</p>
      </div>
      {action}
    </div>
  )
}

export function ResultCount({
  total,
  shown,
  label = 'results',
}: {
  total: number
  shown?: number
  label?: string
}) {
  const singular = label
    .replace(/entries$/, 'entry')
    .replace(/categories$/, 'category')
    .replace(/proxies$/, 'proxy')
    .replace(/s$/, '')
  return (
    <p className="help" role="status" data-result-count>
      {shown !== undefined && <>Showing {number(shown)} of </>}
      {number(total)} {total === 1 ? singular : label}
    </p>
  )
}

export function Pager({
  total,
  limit,
  offset,
  onChange,
  busy = false,
}: {
  total: number
  limit: number
  offset: number
  onChange: (offset: number) => void
  busy?: boolean
}) {
  const lastOffset = Math.max(0, Math.ceil(total / limit) - 1) * limit
  return (
    <nav aria-label="Pagination" className="flex flex-wrap items-center justify-between gap-3 py-3">
      <p className="text-xs text-muted-foreground" aria-live="polite">
        {total === 0
          ? 'No results'
          : offset >= total
            ? `No results on this page · ${number(total)} total`
            : `${number(offset + 1)}–${number(Math.min(offset + limit, total))} of ${number(total)}`}
      </p>
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy || offset === 0}
          onClick={() => onChange(0)}
        >
          First
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy || offset === 0}
          onClick={() => onChange(Math.max(0, Math.min(lastOffset, offset - limit)))}
        >
          <ArrowLeftIcon />
          Previous
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy || offset + limit >= total}
          onClick={() => onChange(offset + limit)}
        >
          Next
          <ArrowRightIcon />
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy || total === 0 || offset === lastOffset}
          onClick={() => onChange(lastOffset)}
        >
          Last
        </Button>
      </div>
    </nav>
  )
}

export function ClearFiltersButton({
  active = true,
  onClear,
}: {
  active?: boolean
  onClear: () => void
}) {
  return (
    <Button type="button" variant="outline" disabled={!active} onClick={onClear}>
      <XIcon />
      Clear filters
    </Button>
  )
}

export function ListEmpty({
  title,
  description,
  filtered = false,
  offset = 0,
  onFirstPage,
  action,
}: {
  title: string
  description: string
  filtered?: boolean
  offset?: number
  onFirstPage: () => void
  action?: ReactNode
}) {
  return (
    <Empty
      icon={filtered ? MagnifyingGlassIcon : undefined}
      title={offset > 0 ? 'No results on this page' : title}
      description={
        offset > 0
          ? 'This page has no results. Return to the first page to check the current view.'
          : filtered
            ? 'No results match these filters. Clear them to broaden the search.'
            : description
      }
      action={
        offset > 0 ? (
          <Button type="button" variant="outline" onClick={onFirstPage}>
            Return to first page
          </Button>
        ) : (
          action
        )
      }
    />
  )
}

export function RunStatusBadge({
  run,
}: {
  run: Pick<Run, 'status' | 'cancel_requested' | 'pause_requested'>
}) {
  const active = run.status === 'queued' || run.status === 'running'
  const pausing = active && run.pause_requested && !run.cancel_requested
  const label =
    active && run.cancel_requested
      ? 'Cancellation requested'
      : pausing
        ? 'Pausing…'
        : statusLabels[run.status]
  return (
    <Badge
      variant={
        run.status === 'failed' || (active && run.cancel_requested)
          ? 'destructive'
          : run.status === 'paused' || pausing
            ? 'warning'
            : run.status === 'succeeded'
              ? 'success'
              : run.status === 'running'
                ? 'info'
                : 'secondary'
      }
    >
      {active && <span aria-hidden="true" className="size-1.5 rounded-full bg-current" />}
      {label}
    </Badge>
  )
}

export function ConfirmAction({
  trigger,
  title,
  description,
  label = 'Confirm',
  action,
  variant = 'default',
}: {
  trigger: ReactNode
  title: string
  description: ReactNode
  label?: string
  action: () => Promise<unknown>
  variant?: 'default' | 'warning' | 'destructive'
}) {
  const [open, setOpen] = useState(false)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const confirming = useRef(false)
  // Keep the visible decision and the captured write on the same revision while
  // live reads update the underlying list. Reopening reviews the latest values.
  const [selected, setSelected] = useState({ action, title, description, label, variant })
  async function confirm() {
    if (confirming.current) return
    confirming.current = true
    setPending(true)
    setError(null)
    try {
      await selected.action()
      setOpen(false)
    } catch (caught) {
      setError(caught)
    } finally {
      confirming.current = false
      setPending(false)
    }
  }
  return (
    <AlertDialog
      open={open}
      onOpenChange={(value) => {
        if (!confirming.current) {
          if (value) setSelected({ action, title, description, label, variant })
          setOpen(value)
          setError(null)
        }
      }}
    >
      <AlertDialogTrigger asChild>{trigger}</AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{selected.title}</AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="text-sm leading-6 text-muted-foreground">{selected.description}</div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <ErrorNotice error={error} />
        <AlertDialogFooter>
          <AlertDialogCancel className="min-h-11 sm:min-h-9" disabled={pending}>
            Back
          </AlertDialogCancel>
          <Button
            variant={selected.variant}
            className="min-h-11 sm:min-h-9"
            disabled={pending}
            onClick={() => void confirm()}
          >
            {pending ? 'Processing…' : selected.label}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
