import { PencilSimpleIcon, XIcon } from '@phosphor-icons/react'
import {
  createContext,
  useCallback,
  useContext,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useEffect,
  type ReactNode,
} from 'react'
import { useBlocker } from 'react-router-dom'
import { useSessionExpired } from '@/components/public-connection-fields'
import { changesSettingsEditor, type SettingsEditorIdentity } from './settings-editor'
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'

type Entry = { label: string; scope: string | null; pending: boolean }
type Guard = {
  register: (id: string, entry: Entry | null) => () => void
  discard: (scope: string | null, proceed: () => void) => void
}
const GuardContext = createContext<Guard | null>(null)
export const UnsavedChangesScope = createContext<string | null>(null)

// Keep only form state flags in memory. Credential values and drafts are never
// copied into browser storage or into the navigation confirmation.
export function UnsavedChangesProvider({ children }: { children: ReactNode }) {
  const entries = useRef(new Map<string, Entry>())
  const keepEditing = useRef<HTMLButtonElement>(null)
  const [, refresh] = useState(0)
  const [closing, setClosing] = useState<{ scope: string | null; proceed: () => void } | null>(null)
  const register = useCallback((id: string, entry: Entry | null) => {
    if (entry) entries.current.set(id, entry)
    else entries.current.delete(id)
    refresh((value) => value + 1)
    return () => {
      if (entries.current.delete(id)) refresh((value) => value + 1)
    }
  }, [])
  const discard = useCallback((scope: string | null, proceed: () => void) => {
    if ([...entries.current.values()].some((entry) => scope === 'all' || entry.scope === scope)) {
      setClosing({ scope, proceed })
    } else proceed()
  }, [])
  const guard = useMemo(() => ({ register, discard }), [register, discard])
  const samePageBlocked = useRef(false)
  const blocker = useBlocker(
    useCallback(({ currentLocation, nextLocation }) => {
      if (!entries.current.size) return false
      if (currentLocation.pathname !== nextLocation.pathname) {
        samePageBlocked.current = false
        return true
      }
      if (![...entries.current.values()].some((entry) => entry.scope !== null)) return false
      const editor = document.querySelector<HTMLElement>(
        '[data-settings-editor]:not([data-settings-editor="source"])',
      )
      if (!editor?.dataset.settingsEditor || !editor.dataset.settingsEntity) return false
      const blocked = changesSettingsEditor(currentLocation, nextLocation, {
        entity: editor.dataset.settingsEditor as SettingsEditorIdentity['entity'],
        id: editor.dataset.settingsEntity,
      })
      samePageBlocked.current = blocked
      return blocked
    }, []),
  )
  const samePageBlocker = !closing && blocker.state === 'blocked' && samePageBlocked.current
  const dirty = [...entries.current.values()].filter((entry) =>
    closing
      ? closing.scope === 'all' || entry.scope === closing.scope
      : !samePageBlocker || entry.scope !== null,
  )
  const pending = dirty.some((entry) => entry.pending)
  const open = closing !== null || blocker.state === 'blocked'
  const cancel = useCallback(() => {
    setClosing(null)
    if (blocker.state === 'blocked') blocker.reset()
  }, [blocker])
  useSessionExpired(cancel)
  useEffect(() => {
    if (!entries.current.size) return
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (!entries.current.size) return
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', beforeUnload)
    return () => window.removeEventListener('beforeunload', beforeUnload)
  }, [entries.current.size])
  useEffect(() => {
    if (dirty.length) return
    // A successful save can finish while a requested navigation is waiting.
    // Once no draft remains, continue without asking to discard saved values.
    if (closing) {
      setClosing(null)
      closing.proceed()
    } else if (blocker.state === 'blocked') blocker.proceed()
  }, [dirty.length, closing, blocker])
  return (
    <GuardContext.Provider value={guard}>
      {children}
      <AlertDialog
        open={open}
        onOpenChange={(value) => {
          if (!value) cancel()
        }}
      >
        <AlertDialogContent
          onOpenAutoFocus={(event) => {
            event.preventDefault()
            keepEditing.current?.focus()
          }}
        >
          <AlertDialogHeader>
            <AlertDialogTitle>Unsaved changes</AlertDialogTitle>
            <AlertDialogDescription>
              {pending
                ? 'A save is still in progress. Wait for it to finish before leaving.'
                : `Changes in ${[...new Set(dirty.map((entry) => entry.label))].join(', ') || 'this form'} have not been saved. Keep editing or discard them to continue.`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <Button ref={keepEditing} variant="outline" onClick={cancel}>
              <PencilSimpleIcon aria-hidden="true" />
              Keep editing
            </Button>
            <Button
              variant="warning"
              disabled={pending}
              onClick={() => {
                const proceed = closing?.proceed
                setClosing(null)
                if (proceed) proceed()
                else if (blocker.state === 'blocked') blocker.proceed()
              }}
            >
              <XIcon aria-hidden="true" />
              Discard changes
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </GuardContext.Provider>
  )
}

export function useUnsavedChanges(
  dirty: boolean,
  label: string,
  pending = false,
  explicitScope?: string,
) {
  const guard = useContext(GuardContext)
  const inheritedScope = useContext(UnsavedChangesScope)
  const scope = explicitScope ?? inheritedScope
  const id = useId()
  useLayoutEffect(
    () => guard?.register(id, dirty || pending ? { label, scope, pending } : null),
    [guard, id, dirty, label, pending, scope],
  )
  // Successful saves may navigate before React commits the clean form state.
  // Release this form immediately only after its write has completed.
  return useCallback(() => {
    guard?.register(id, null)
  }, [guard, id])
}

export function useDiscardChanges(scope?: string | null) {
  const guard = useContext(GuardContext)
  const inherited = useContext(UnsavedChangesScope)
  const selected = scope === undefined ? inherited : scope
  return useCallback(
    (proceed: () => void) => {
      if (guard) guard.discard(selected, proceed)
      else proceed()
    },
    [guard, selected],
  )
}
