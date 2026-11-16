import { createContext, useContext, useRef, useState, type ReactNode } from 'react'
import { Link, useLocation, useNavigate, type LinkProps } from 'react-router-dom'
import { useViewSearchParams } from '@/lib/detail-navigation'
import type { Torrent } from '@/lib/types'

const occurrenceKeys = [
  'occurrence_tab',
  'history_offset',
  'history_limit',
  'related_offset',
  'related_limit',
]

type OccurrenceTab = 'fields' | 'history' | 'related'

function occurrenceSearch(search: URLSearchParams, id?: string, tab?: OccurrenceTab) {
  const next = new URLSearchParams(search)
  for (const key of occurrenceKeys) next.delete(key)
  if (id) {
    next.set('occurrence', id)
    if (tab) next.set('occurrence_tab', tab)
  } else next.delete('occurrence')
  return next
}

const OccurrenceNavigation = createContext<{
  id: string
  torrent?: Torrent
  open: (id: string, target: HTMLElement, torrent?: Torrent, tab?: OccurrenceTab) => void
  close: () => void
  returnFocus: () => void
} | null>(null)

// Occurrences are overlays on the current workspace. Their query parameters
// retain deep links, browser navigation and the originating page's filters.
export function OccurrenceNavigationProvider({ children }: { children: ReactNode }) {
  const [search, setSearch] = useViewSearchParams()
  const location = useLocation()
  const navigate = useNavigate()
  const id = search.get('occurrence') ?? ''
  const [selected, setSelected] = useState<Torrent>()
  const trigger = useRef<HTMLElement | null>(null)
  return (
    <OccurrenceNavigation.Provider
      value={{
        id,
        torrent: selected?.occurrence_id === id ? selected : undefined,
        open: (nextID, target, torrent, tab) => {
          if (!id) trigger.current = target
          setSelected(torrent)
          const state = location.state && typeof location.state === 'object' ? location.state : {}
          setSearch(occurrenceSearch(search, nextID, tab), {
            replace: Boolean(id),
            preventScrollReset: true,
            state: {
              ...state,
              occurrenceReturn: id
                ? state.occurrenceReturn
                : {
                    pathname: location.pathname,
                    search: location.search,
                    index: window.history.state?.idx,
                  },
            },
          })
        },
        close: () => {
          const origin = location.state?.occurrenceReturn
          const next = occurrenceSearch(search)
          setSelected(undefined)
          // A UI-opened window owns one history entry. Closing it must not
          // leave an extra copy of the list behind. Direct links have no parent.
          if (
            origin &&
            origin.pathname === location.pathname &&
            typeof origin.search === 'string' &&
            Number.isSafeInteger(origin.index) &&
            origin.index >= 0 &&
            window.history.state?.idx === origin.index + 1 &&
            new URLSearchParams(origin.search).toString() === next.toString()
          ) {
            void navigate(-1)
          } else setSearch(next, { replace: true, preventScrollReset: true })
        },
        returnFocus: () => {
          if (trigger.current?.isConnected) trigger.current.focus({ preventScroll: true })
          else
            document
              .querySelector<HTMLElement>('main input, main button, main a')
              ?.focus({ preventScroll: true })
        },
      }}
    >
      {children}
    </OccurrenceNavigation.Provider>
  )
}

export function useOccurrenceNavigation() {
  const value = useContext(OccurrenceNavigation)
  if (!value) throw new Error('Occurrence links require a workspace navigation provider.')
  return value
}

export function OccurrenceLink({
  occurrenceId,
  torrent,
  tab,
  children,
  onClick,
  ...props
}: Omit<LinkProps, 'to'> & { occurrenceId: string; torrent?: Torrent; tab?: OccurrenceTab }) {
  const location = useLocation()
  const navigation = useOccurrenceNavigation()
  const search = occurrenceSearch(new URLSearchParams(location.search), occurrenceId, tab)
  return (
    <Link
      {...props}
      to={`${location.pathname}?${search}`}
      onClick={(event) => {
        onClick?.(event)
        if (
          !event.defaultPrevented &&
          event.button === 0 &&
          !event.ctrlKey &&
          !event.metaKey &&
          !event.shiftKey &&
          !event.altKey
        ) {
          event.preventDefault()
          navigation.open(occurrenceId, event.currentTarget, torrent, tab)
        }
      }}
    >
      {children}
    </Link>
  )
}
