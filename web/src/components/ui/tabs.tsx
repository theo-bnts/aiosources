import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'
import { Tabs as TabsPrimitive } from 'radix-ui'

const TabsOrientation = React.createContext<'horizontal' | 'vertical'>('horizontal')

function Tabs({
  className,
  orientation = 'horizontal',
  ...props
}: React.ComponentProps<typeof TabsPrimitive.Root>) {
  return (
    <TabsOrientation.Provider value={orientation}>
      <TabsPrimitive.Root
        data-slot="tabs"
        data-orientation={orientation}
        orientation={orientation}
        className={cn('group/tabs flex gap-2 data-[orientation=horizontal]:flex-col', className)}
        {...props}
      />
    </TabsOrientation.Provider>
  )
}

const tabsListVariants = cva(
  'group/tabs-list inline-flex h-(--control-height) min-w-full w-max items-center justify-start rounded-lg p-[3px] text-muted-foreground data-[orientation=vertical]:h-fit data-[orientation=vertical]:min-w-0 data-[orientation=vertical]:flex-col data-[variant=line]:rounded-none',
  {
    variants: {
      variant: {
        default: 'bg-muted',
        line: 'gap-1 bg-transparent',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  },
)

function TabsList({
  className,
  variant = 'default',
  containerClassName,
  ref,
  ...props
}: React.ComponentProps<typeof TabsPrimitive.List> &
  VariantProps<typeof tabsListVariants> & { containerClassName?: string }) {
  const orientation = React.useContext(TabsOrientation)
  const viewport = React.useRef<HTMLDivElement | null>(null)
  const list = React.useRef<HTMLDivElement | null>(null)
  const setRef = React.useCallback(
    (element: HTMLDivElement | null) => {
      list.current = element
      if (typeof ref === 'function') return ref(element)
      if (ref) ref.current = element
    },
    [ref],
  )
  React.useEffect(() => {
    const element = list.current
    if (!element) return
    const revealActiveTab = () => {
      if (element.dataset.orientation !== 'horizontal') return
      const active = element.querySelector<HTMLElement>('[role="tab"][data-state="active"]')
      if (!active) return
      const scroller = viewport.current ?? element
      const boundary = scroller.getBoundingClientRect()
      const tab = active.getBoundingClientRect()
      const left = tab.left - boundary.left - scroller.clientLeft
      const right = tab.right - boundary.left - scroller.clientLeft - scroller.clientWidth
      // Scroll only this bar: selecting a tab must not move the document.
      if (left < 0) scroller.scrollLeft += left
      else if (right > 0) scroller.scrollLeft += right
    }
    revealActiveTab()
    const selection = new MutationObserver(revealActiveTab)
    selection.observe(element, { subtree: true, attributes: true, attributeFilter: ['data-state'] })
    const size = new ResizeObserver(revealActiveTab)
    size.observe(element)
    if (viewport.current) size.observe(viewport.current)
    for (const tab of element.querySelectorAll('[role="tab"]')) size.observe(tab)
    return () => {
      selection.disconnect()
      size.disconnect()
    }
  }, [])
  const content = (
    <TabsPrimitive.List
      ref={setRef}
      data-slot="tabs-list"
      data-variant={variant}
      className={cn(tabsListVariants({ variant }), className)}
      {...props}
    />
  )
  if (orientation === 'vertical') return content
  return (
    <div
      ref={viewport}
      data-slot="tabs-scroll"
      className={cn('min-w-0 max-w-full overflow-x-auto overflow-y-hidden', containerClassName)}
    >
      {content}
    </div>
  )
}

function TabsTrigger({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      data-slot="tabs-trigger"
      className={cn(
        "relative inline-flex h-full min-h-[calc(var(--control-height)-6px)] shrink-0 flex-1 items-center justify-center gap-1.5 rounded-md border border-transparent px-2 py-1 text-sm font-medium whitespace-nowrap text-foreground/60 transition-[color,background-color,border-color,box-shadow,opacity] group-data-[orientation=vertical]/tabs:w-full group-data-[orientation=vertical]/tabs:justify-start hover:text-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-1 focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-50 group-data-[variant=default]/tabs-list:data-[state=active]:shadow-sm group-data-[variant=line]/tabs-list:data-[state=active]:shadow-none dark:text-muted-foreground dark:hover:text-foreground [&_svg]:pointer-events-none [&_svg:not([class*='size-'])]:size-4",
        'group-data-[variant=line]/tabs-list:bg-transparent group-data-[variant=line]/tabs-list:data-[state=active]:bg-transparent dark:group-data-[variant=line]/tabs-list:data-[state=active]:border-transparent dark:group-data-[variant=line]/tabs-list:data-[state=active]:bg-transparent',
        'data-[state=active]:bg-background data-[state=active]:text-foreground dark:data-[state=active]:border-input dark:data-[state=active]:bg-input/30 dark:data-[state=active]:text-foreground',
        'after:absolute after:hidden after:bg-foreground after:opacity-0 after:transition-opacity group-data-[variant=line]/tabs-list:after:block group-data-[orientation=horizontal]/tabs:after:inset-x-0 group-data-[orientation=horizontal]/tabs:after:bottom-0 group-data-[orientation=horizontal]/tabs:after:h-0.5 group-data-[orientation=vertical]/tabs:after:inset-y-0 group-data-[orientation=vertical]/tabs:after:right-0 group-data-[orientation=vertical]/tabs:after:w-0.5 group-data-[variant=line]/tabs-list:data-[state=active]:after:opacity-100',
        className,
      )}
      {...props}
    />
  )
}

function TabsContent({ className, ...props }: React.ComponentProps<typeof TabsPrimitive.Content>) {
  return (
    <TabsPrimitive.Content
      data-slot="tabs-content"
      className={cn('flex-1 outline-none', className)}
      {...props}
    />
  )
}

export { Tabs, TabsList, TabsTrigger, TabsContent, tabsListVariants }
