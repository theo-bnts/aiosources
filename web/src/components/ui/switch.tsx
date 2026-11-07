'use client'

import * as React from 'react'
import { cn } from '@/lib/utils'
import { Switch as SwitchPrimitive } from 'radix-ui'

function Switch({
  className,
  size = 'default',
  ...props
}: React.ComponentProps<typeof SwitchPrimitive.Root> & {
  size?: 'sm' | 'default'
}) {
  // A confirmation trigger may supply its own open/closed data-state.
  // aria-checked remains the switch's authoritative visual state.
  return (
    <SwitchPrimitive.Root
      data-slot="switch"
      data-size={size}
      className={cn(
        'peer group/switch relative inline-flex size-(--control-height) shrink-0 items-center justify-center rounded-md outline-none transition-[color,box-shadow] focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
      {...props}
    >
      <span
        data-slot="switch-track"
        aria-hidden="true"
        className="pointer-events-none flex shrink-0 items-center rounded-full border border-transparent shadow-xs transition-colors group-data-[size=default]/switch:h-[1.15rem] group-data-[size=default]/switch:w-8 group-data-[size=sm]/switch:h-3.5 group-data-[size=sm]/switch:w-6 group-aria-[checked=true]/switch:bg-primary group-aria-[checked=false]/switch:bg-input dark:group-aria-[checked=false]/switch:bg-input/80"
      >
        <SwitchPrimitive.Thumb
          data-slot="switch-thumb"
          className="pointer-events-none block rounded-full bg-background ring-0 transition-transform group-data-[size=default]/switch:size-4 group-data-[size=sm]/switch:size-3 data-[state=checked]:translate-x-[calc(100%-2px)] data-[state=unchecked]:translate-x-0 dark:data-[state=checked]:bg-primary-foreground dark:data-[state=unchecked]:bg-foreground"
        />
      </span>
    </SwitchPrimitive.Root>
  )
}

export { Switch }
