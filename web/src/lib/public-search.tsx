import { createContext, useContext } from 'react'
import type { PublicInfo } from '@/components/public-connection-fields'

export type PublicSearchAccess = {
  configuration: string
  credential: string
  scope: string
  info: PublicInfo
}
export const PublicSearchContext = createContext<PublicSearchAccess | null>(null)
export const usePublicSearch = () => useContext(PublicSearchContext)
