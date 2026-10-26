import * as React from 'react'
import { cn } from '@/lib/utils'
import { Label as LabelPrimitive } from 'radix-ui'

function Label({ className, ...props }: React.ComponentProps<typeof LabelPrimitive.Root>) {
  return (
    <LabelPrimitive.Root
      data-slot="label"
      className={cn(
        'flex min-w-0 items-center gap-2 text-sm leading-5 font-medium select-none [overflow-wrap:anywhere] group-data-[disabled=true]:pointer-events-none group-data-[disabled=true]:opacity-50 peer-disabled:cursor-not-allowed peer-disabled:opacity-50',
        className,
      )}
      {...props}
    />
  )
}

export { Label }
