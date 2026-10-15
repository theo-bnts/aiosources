import { messageText } from '@/lib/format'
import { ConnectionError } from '@/lib/api'
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { CheckCircleIcon, WarningCircleIcon, XIcon } from '@phosphor-icons/react'
import { createPortal } from 'react-dom'
import { Toast } from 'radix-ui'
import { RequestNotices, type RequestIssue } from '@/lib/request-notices'
import { Notifications, type Notification, type Notify } from '@/lib/notifications'
import { ErrorNotice } from '@/components/common'
import { Button } from '@/components/ui/button'

export function NotificationProvider({ children }: { children: ReactNode }) {
  const [issues, setIssues] = useState<Map<string, RequestIssue>>(() => new Map())
  const [dismissed, setDismissed] = useState<unknown>(null)
  const [messages, setMessages] = useState<Array<Notification & { id: number }>>([])
  const nextID = useRef(0)
  useEffect(() => {
    const clear = () => {
      setMessages([])
      setIssues(new Map())
      setDismissed(null)
    }
    window.addEventListener('session-expired', clear)
    return () => window.removeEventListener('session-expired', clear)
  }, [])
  const notify = useCallback<Notify>((notice) => {
    const message = typeof notice === 'string' ? { message: notice } : notice
    if (!message.message) return
    const id = ++nextID.current
    setMessages((current) => [...current.slice(-2), { ...message, id }])
  }, [])
  const register = useCallback((id: string, issue: RequestIssue) => {
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
  const connection = issue?.error instanceof ConnectionError
  return (
    <Toast.Provider duration={8000}>
      <Notifications.Provider value={notify}>
        <RequestNotices.Provider value={register}>
          {children}
          {issue && (
            <Toast.Root
              data-request-notice
              data-connection-notice={connection || undefined}
              duration={Infinity}
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
                  aria-label={
                    connection ? 'Dismiss connection notification' : 'Dismiss notification'
                  }
                >
                  <XIcon aria-hidden="true" />
                </Button>
              </Toast.Close>
            </Toast.Root>
          )}
          {messages.map((notice) => {
            const Icon = notice.warning ? WarningCircleIcon : CheckCircleIcon
            return (
              <Toast.Root
                key={notice.id}
                data-notification
                role="status"
                aria-live="off"
                defaultOpen
                onOpenChange={(open) => {
                  if (!open)
                    setMessages((current) => current.filter((item) => item.id !== notice.id))
                }}
                className="relative flex items-start gap-3 rounded-md border bg-card p-4 pr-12 text-card-foreground shadow-sm outline-none"
              >
                <Icon
                  aria-hidden="true"
                  className={`mt-0.5 size-5 shrink-0 ${notice.warning ? 'text-amber-300' : 'text-primary'}`}
                />
                <Toast.Description className="min-w-0 break-words text-sm leading-6">
                  {messageText(notice.message)}
                </Toast.Description>
                <Toast.Close asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="absolute top-2 right-2"
                    aria-label="Dismiss notification"
                  >
                    <XIcon aria-hidden="true" />
                  </Button>
                </Toast.Close>
              </Toast.Root>
            )
          })}
          {createPortal(
            <Toast.Viewport
              aria-live="polite"
              className="fixed right-4 bottom-4 z-70 flex w-[calc(100%-2rem)] max-w-md list-none flex-col gap-3 outline-none"
            />,
            document.body,
          )}
        </RequestNotices.Provider>
      </Notifications.Provider>
    </Toast.Provider>
  )
}
