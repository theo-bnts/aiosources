import type { MatchDiagnostic } from './aiosource-types'

export const identificationResults = {
  matched: { label: 'Identified', variant: 'success' },
  ambiguous: { label: 'Needs review', variant: 'warning' },
  unmatched: { label: 'Not identified', variant: 'secondary' },
  pending: { label: 'Awaiting identification', variant: 'info' },
} as const satisfies Record<MatchDiagnostic['status'], { label: string; variant: string }>

export const identificationStatuses = Object.keys(identificationResults) as Array<
  keyof typeof identificationResults
>
