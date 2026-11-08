import type { ComponentProps, ReactNode } from 'react'
import { CaretRightIcon } from '@phosphor-icons/react'
import { cn } from '@/lib/utils'
import { InfoTip } from '@/components/ui/info-tip'

type DisclosureProps = Omit<ComponentProps<'details'>, 'title' | 'children'> & {
  title: ReactNode
  children: ReactNode
  variant?: 'card' | 'inline'
  onOpenChange?: (open: boolean) => void
  help?: ReactNode
  helpLabel?: string
}

export function Disclosure({
  title,
  children,
  variant = 'card',
  className,
  onOpenChange,
  help,
  helpLabel,
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
        inline ? '' : 'rounded-md border border-border bg-card',
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
          'text-sm leading-5',
          inline ? 'min-h-(--control-height)' : 'min-h-[calc(var(--control-height)-2px)] px-4 py-2',
        )}
      >
        <CaretRightIcon aria-hidden="true" className="size-3 shrink-0 transition-transform" />
        <span className="flex min-w-0 flex-1 items-center gap-1.5">
          <span className="min-w-0 break-words">{title}</span>
          {help && (
            <InfoTip label={helpLabel ?? (typeof title === 'string' ? title : 'this section')}>
              {help}
            </InfoTip>
          )}
        </span>
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
