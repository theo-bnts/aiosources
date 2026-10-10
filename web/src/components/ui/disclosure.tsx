import type { ComponentProps, ReactNode } from 'react'
import { CaretRightIcon } from '@phosphor-icons/react'
import { cn } from '@/lib/utils'

type DisclosureProps = Omit<ComponentProps<'details'>, 'title' | 'children'> & {
  title: ReactNode
  children: ReactNode
  variant?: 'card' | 'inline'
  onOpenChange?: (open: boolean) => void
}

export function Disclosure({
  title,
  children,
  variant = 'card',
  className,
  onOpenChange,
  ...props
}: DisclosureProps) {
  const inline = variant === 'inline'
  return (
    <details
      {...props}
      data-slot="disclosure"
      data-variant={variant}
      className={cn(
        '[&[open]>summary>svg]:rotate-90',
        inline ? 'text-xs' : 'rounded-md border border-border bg-card',
        className,
      )}
    >
      <summary
        onClick={
          onOpenChange
            ? (event) => {
                event.preventDefault()
                onOpenChange(!props.open)
              }
            : undefined
        }
        data-slot="disclosure-trigger"
        className={cn(
          'flex cursor-pointer list-none items-center gap-2 font-medium [&::-webkit-details-marker]:hidden',
          inline ? 'min-h-5 text-xs leading-5' : 'min-h-11 px-4 py-2.5 text-sm leading-5',
        )}
      >
        <CaretRightIcon aria-hidden="true" className="size-3 shrink-0 transition-transform" />
        <span className="min-w-0 flex-1 break-words">{title}</span>
      </summary>
      <div
        data-slot="disclosure-content"
        className={cn(
          'min-w-0',
          inline ? 'space-y-3 pt-3' : 'space-y-4 border-t border-border p-4',
        )}
      >
        {children}
      </div>
    </details>
  )
}
