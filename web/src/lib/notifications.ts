import { createContext, useContext } from 'react'

export type Notification = { message: string; warning?: boolean }
export type Notify = (notice: string | Notification) => void

export const Notifications = createContext<Notify | null>(null)

export function useNotification(): Notify {
  const notify = useContext(Notifications)
  if (!notify) throw new Error('Notifications require NotificationProvider.')
  return notify
}
