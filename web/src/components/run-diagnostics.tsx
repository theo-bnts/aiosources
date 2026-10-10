import { number } from '@/lib/format'

const reasons: Record<string, string> = {
  invalid_position: 'Invalid pagination position',
  position_mismatch: 'Returned position differs from requested position',
  invalid_total: 'Invalid advertised total',
  total_changed: 'Advertised total changed beyond the allowed policy',
  records_exceed_total: 'Records exceed the advertised total',
  empty_before_total: 'Empty page before the advertised total',
  invalid_continuation: 'Invalid or disallowed continuation',
  continuation_repeated: 'Repeated continuation',
  continuation_mismatch: 'Next position skips or repeats records',
  premature_end: 'Pagination ended before the advertised total',
  unexpected_continuation: 'Continuation contradicts page completion',
  pagination_overflow: 'Pagination position exceeds the supported range',
  publication_order_invalid: 'Publication dates are not in descending order',
}

export const paginationRemediation =
  'Check the source pagination fields, ordering and total policy. If the upstream response is corrected, Resume retries the rejected page. If source JSON must change, start a new run: Resume keeps the original configuration.'

export function RunFailureDiagnostic({ data }: { data: Record<string, unknown> | undefined }) {
  const reason = data?.failure_reason
  if (typeof reason !== 'string' || !Object.hasOwn(reasons, reason)) return null
  const context = [
    ['http_status', 'HTTP'],
    ['requested_page', 'Requested page'],
    ['record_index', 'Record position on page'],
    ['offset', 'Requested offset'],
    ['expected_total', 'Expected total'],
    ['actual_total', 'Actual total'],
    ['expected_position', 'Expected position'],
    ['actual_position', 'Actual position'],
  ].flatMap(([key, label]) => {
    const value = data?.[key]
    return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
      ? [`${label}: ${number(value)}`]
      : []
  })
  return (
    <div className="mt-2 space-y-2 text-xs leading-5">
      <p className="font-medium">{reasons[reason]}</p>
      {context.length > 0 && <p className="font-mono">{context.join(' · ')}</p>}
      <p className="text-muted-foreground">Checkpoint unchanged. {paginationRemediation}</p>
    </div>
  )
}
