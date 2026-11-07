import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'
import { Slot } from 'radix-ui'

const buttonVariants = cva(
  "inline-flex min-h-(--control-height) shrink-0 items-center justify-center gap-2 rounded-md text-base font-medium whitespace-nowrap transition-all outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 md:text-sm [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:bg-primary/90',
        destructive:
          'border border-destructive/30 bg-destructive/10 text-destructive hover:bg-destructive/20 focus-visible:ring-destructive/30',
        warning:
          'border border-warning/30 bg-warning/10 text-warning-foreground hover:bg-warning/20 focus-visible:ring-warning/30',
        outline:
          'border bg-background shadow-xs hover:bg-accent hover:text-accent-foreground dark:border-input dark:bg-input/30 dark:hover:bg-input/50',
        secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
        ghost: 'hover:bg-accent hover:text-accent-foreground dark:hover:bg-accent/50',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-(--control-height) px-4 py-2 has-[>svg]:px-3',
        xs: 'h-(--control-height) gap-1 px-2 has-[>svg]:px-1.5',
        sm: 'h-(--control-height) gap-1.5 px-3 has-[>svg]:px-2.5',
        lg: 'h-(--control-height) px-6 has-[>svg]:px-4',
        icon: 'size-(--control-height)',
        'icon-xs': 'size-(--control-height)',
        'icon-sm': 'size-(--control-height)',
        'icon-lg': 'size-(--control-height)',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
)

function Button({
  className,
  variant = 'default',
  size = 'default',
  asChild = false,
  ...props
}: React.ComponentProps<'button'> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
  }) {
  const Comp = asChild ? Slot.Root : 'button'

  return (
    <Comp
      data-slot="button"
      data-variant={variant}
      data-size={size}
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Button, buttonVariants }
