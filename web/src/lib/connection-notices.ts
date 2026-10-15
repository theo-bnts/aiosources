import { createContext } from 'react'
import type { ConnectionError } from './api'

export type ConnectionIssue = { error: ConnectionError; retry?: () => void }

export const ConnectionNotices = createContext<
  ((id: string, issue: ConnectionIssue) => () => void) | null
>(null)
