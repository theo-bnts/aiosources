import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { XIcon } from '@phosphor-icons/react'
import { createPortal } from 'react-dom'
import { Toast } from 'radix-ui'
import { ConnectionNotices, type ConnectionIssue } from '@/lib/connection-notices'
import { ErrorNotice } from '@/components/common'
import { Button } from '@/components/ui/button'

export function ConnectionNoticeProvider({ children }: { children: ReactNode }) {
  const [issues, setIssues] = useState<Map<string, ConnectionIssue>>(() => new Map())
  const [dismissed, setDismissed] = useState<unknown>(null)
  const register = useCallback((id: string, issue: ConnectionIssue) => {
    setIssues((current) => new Map(current).set(id, issue))
    return () => {
      setIssues((current) => {
        if (!current.has(id)) return current
        const next = new Map(current)
        next.delete(id)
        return next
      })
    }
  }, [])
  const issue = useMemo(() => [...issues.values()].at(-1), [issues])
  return (
    <Toast.Provider duration={Infinity}>
      <ConnectionNotices.Provider value={register}>
        {children}
        {issue && (
          <Toast.Root
            data-connection-notice
            role="alert"
            aria-live="off"
            open={dismissed !== issue.error}
            onOpenChange={(open) => {
              if (!open) setDismissed(issue.error)
            }}
            className="relative rounded-md bg-card outline-none"
          >
            <ErrorNotice error={issue.error} retry={issue.retry} presentation="notification" />
            <Toast.Close asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                className="absolute top-2 right-2"
                aria-label="Dismiss connection notification"
              >
                <XIcon aria-hidden="true" />
              </Button>
            </Toast.Close>
          </Toast.Root>
        )}
        {createPortal(
          <Toast.Viewport
            aria-live="polite"
            className="fixed right-4 bottom-4 z-70 w-[calc(100%-2rem)] max-w-md list-none outline-none"
          />,
          document.body,
        )}
      </ConnectionNotices.Provider>
    </Toast.Provider>
  )
}
