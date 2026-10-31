import { PublicAccessSettings } from '@/components/public-access-settings'
import { PublicCatalogSettings } from '@/components/public-catalog-settings'
export default function PublicAccessPage() {
  return (
    <div className="page">
      <PublicAccessSettings />
      <PublicCatalogSettings />
    </div>
  )
}
