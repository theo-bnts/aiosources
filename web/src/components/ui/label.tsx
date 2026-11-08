import * as React from 'react'
import { cn } from '@/lib/utils'
import { Label as LabelPrimitive } from 'radix-ui'
import { InfoTip } from '@/components/ui/info-tip'

function Label({
  className,
  help,
  helpLabel,
  ...props
}: React.ComponentProps<typeof LabelPrimitive.Root> & {
  help?: React.ReactNode
  helpLabel?: string
}) {
  const label = (
    <LabelPrimitive.Root
      data-slot="label"
      className={cn(
        'flex min-h-6 min-w-0 items-center gap-2 text-sm leading-5 font-medium select-none [overflow-wrap:anywhere] group-data-[disabled=true]:pointer-events-none group-data-[disabled=true]:opacity-50 peer-disabled:cursor-not-allowed peer-disabled:opacity-50',
        className,
      )}
      {...props}
    />
  )
  return help ? (
    <div className="flex min-w-0 items-center gap-1.5">
      {label}
      <InfoTip
        label={helpLabel ?? (typeof props.children === 'string' ? props.children : 'this field')}
      >
        {help}
      </InfoTip>
    </div>
  ) : (
    label
  )
}

export { Label }
