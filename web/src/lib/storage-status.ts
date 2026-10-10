export function storageStatus(available: boolean, measuredAt: string | undefined, stale = false) {
  if (!available)
    return {
      label: measuredAt ? 'Measurement unavailable' : 'Unavailable',
      variant: measuredAt ? 'warning' : 'destructive',
    } as const
  if (!measuredAt) return { label: 'Awaiting measurement', variant: 'info' } as const
  if (stale) return { label: 'Out of date', variant: 'warning' } as const
  return { label: 'Available', variant: 'success' } as const
}
