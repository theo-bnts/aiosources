import type { ReactNode } from 'react'
import { ArrowSquareOutIcon } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'

export function CredentialPageLink({ href, children }: { href: string; children: ReactNode }) {
  if (!/^https:\/\/[^\s"'<>]+$/.test(href)) return null
  return (
    <div>
      <Button asChild variant="outline">
        <a href={href} target="_blank" rel="noopener noreferrer">
          <ArrowSquareOutIcon aria-hidden="true" />
          {children}
        </a>
      </Button>
    </div>
  )
}
