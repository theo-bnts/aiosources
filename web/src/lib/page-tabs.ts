import { useCallback } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

// Tab changes stay within the current page. Keep public return-view state, but
// let ordinary page/detail navigation retain its normal focus and scroll reset.
export function pageTabState(state: unknown) {
  return {
    ...(state && typeof state === 'object' && !Array.isArray(state) ? state : {}),
    pageTab: true,
  }
}

export function isPageTabNavigation(state: unknown): boolean {
  return Boolean(state && typeof state === 'object' && 'pageTab' in state && state.pageTab === true)
}

export function usePageTabNavigate() {
  const navigate = useNavigate()
  const { state } = useLocation()
  return useCallback(
    // Commit selection before the trigger's focus event can activate it again.
    (to: string) =>
      navigate(to, { state: pageTabState(state), preventScrollReset: true, flushSync: true }),
    [navigate, state],
  )
}
