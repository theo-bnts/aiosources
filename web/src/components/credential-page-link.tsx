import type { ReactNode } from 'react'
import { ArrowSquareOutIcon } from '@phosphor-icons/react'

export function CredentialPageLink({ href, children }: { href: string; children: ReactNode }) {
  if (!/^https:\/\/[^\s"'<>]+$/.test(href)) return null
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex min-h-6 w-fit max-w-full items-center gap-1.5 rounded-sm text-xs text-muted-foreground underline underline-offset-2 hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
    >
      {children}
      <ArrowSquareOutIcon aria-hidden="true" className="size-3 shrink-0" />
    </a>
  )
}
