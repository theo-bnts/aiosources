import * as React from 'react'
import { cn } from '@/lib/utils'
import { EyeIcon, EyeSlashIcon } from '@phosphor-icons/react'

function Input({
  className,
  type,
  revealLabel = 'password',
  ...props
}: React.ComponentProps<'input'> & { revealLabel?: string }) {
  const [visible, setVisible] = React.useState(false)
  const sensitive = type === 'password'
  const input = (
    <input
      type={sensitive && visible ? 'text' : type}
      data-sensitive={sensitive || undefined}
      data-slot="input"
      className={cn(
        'h-9 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-1 text-base shadow-xs transition-[color,box-shadow] outline-none selection:bg-primary selection:text-primary-foreground file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground placeholder:text-muted-foreground disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm dark:bg-input/30',
        'focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50',
        'aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40',
        sensitive && 'pr-11',
        className,
      )}
      {...props}
    />
  )
  if (!sensitive) return input
  return (
    <div className="relative min-w-0 flex-1">
      {input}
      <button
        type="button"
        disabled={props.disabled}
        aria-label={`${visible ? 'Hide' : 'Reveal'} ${revealLabel}`}
        aria-pressed={visible}
        aria-controls={props.id}
        onPointerDown={(event) => event.preventDefault()}
        onClick={() => setVisible(!visible)}
        className="absolute inset-y-0 right-0 flex w-10 items-center justify-center rounded-r-md text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
      >
        {visible ? (
          <EyeSlashIcon className="size-4" aria-hidden="true" />
        ) : (
          <EyeIcon className="size-4" aria-hidden="true" />
        )}
      </button>
    </div>
  )
}

export { Input }
