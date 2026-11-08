import { useEffect, useRef, useState, type ReactNode } from 'react'
import { InfoIcon } from '@phosphor-icons/react'
import { Popover } from 'radix-ui'
import { popupLayer } from '@/lib/popup-layer'

export function InfoTip({ label, children }: { label: string; children: ReactNode }) {
  const trigger = useRef<HTMLButtonElement>(null)
  const content = useRef<HTMLDivElement>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const keyboard = useRef(false)
  const opened = useRef(false)
  const [open, setOpen] = useState(false)
  const [pinned, setPinned] = useState(false)
  const [layer, setLayer] = useState({ dialog: false, top: 8 })
  function cancelClose() {
    clearTimeout(timer.current)
  }
  function changeOpen(next: boolean) {
    cancelClose()
    opened.current = next
    if (!next) setPinned(false)
    if (next) setLayer(popupLayer(trigger.current))
    setOpen(next)
  }
  function closeLater() {
    cancelClose()
    if (pinned) return
    timer.current = setTimeout(() => {
      if (!content.current?.contains(document.activeElement)) changeOpen(false)
    }, 250)
  }
  useEffect(() => () => clearTimeout(timer.current), [])
  return (
    <Popover.Root open={open} onOpenChange={changeOpen}>
      <Popover.Trigger asChild>
        <button
          ref={trigger}
          type="button"
          data-slot="info-trigger"
          aria-label={`About ${label}`}
          className="inline-flex size-6 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          onClick={(event) => {
            keyboard.current = event.detail === 0
            event.stopPropagation()
            if (opened.current && !pinned) {
              // Clicking a hover preview pins it, rather than immediately
              // closing it through the popup trigger's default toggle.
              event.preventDefault()
              cancelClose()
              setPinned(true)
              if (keyboard.current) content.current?.focus({ preventScroll: true })
            } else if (!opened.current) setPinned(true)
          }}
          onKeyDown={(event) => {
            if (event.key === 'Enter' || event.key === ' ') {
              keyboard.current = true
              event.stopPropagation()
            }
          }}
          onPointerEnter={(event) => {
            if (event.pointerType === 'mouse' && !opened.current) {
              keyboard.current = false
              changeOpen(true)
            }
          }}
          onPointerLeave={(event) => {
            if (event.pointerType === 'mouse') closeLater()
          }}
        >
          <InfoIcon aria-hidden="true" className="size-4" />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        {/* The floating positioner must also let hover-preview clicks pass through. */}
        <div className="pointer-events-none">
          <Popover.Content
            ref={content}
            aria-label={`About ${label}`}
            align="start"
            sideOffset={6}
            collisionPadding={{ top: layer.top, bottom: 8, left: 8, right: 8 }}
            className={`${layer.dialog ? 'z-(--layer-dialog-menu)' : 'z-(--layer-menu)'} max-h-[min(24rem,var(--radix-popover-content-available-height))] w-80 max-w-[calc(100vw-2rem)] space-y-3 overflow-y-auto rounded-md border bg-popover p-4 text-sm leading-6 text-popover-foreground shadow-lg outline-none`}
            style={{ pointerEvents: pinned ? 'auto' : 'none' }}
            onOpenAutoFocus={(event) => {
              if (!keyboard.current) event.preventDefault()
            }}
            onCloseAutoFocus={(event) => {
              // Focus restoration is deferred by the popup. A previous hover
              // must not steal focus after this help has already opened again.
              if (!keyboard.current || opened.current) event.preventDefault()
            }}
            onPointerEnter={cancelClose}
            onPointerLeave={closeLater}
            onClick={(event) => event.stopPropagation()}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') event.stopPropagation()
            }}
          >
            {children}
          </Popover.Content>
        </div>
      </Popover.Portal>
    </Popover.Root>
  )
}
