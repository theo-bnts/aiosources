import { createContext } from 'react'
export type RequestIssue = { error: unknown; retry?: () => void }

export const RequestNotices = createContext<
  ((id: string, issue: RequestIssue) => () => void) | null
>(null)
