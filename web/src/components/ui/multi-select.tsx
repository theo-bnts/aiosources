import { DropdownMenu } from 'radix-ui'
import { useRef, useState } from 'react'
import { CheckIcon, CaretDownIcon } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'

export type MultiSelectOption = { value: string; label: string; group?: string }

// An empty selection deliberately means all, including future options.
export function MultiSelect({
  id,
  label,
  allLabel,
  value,
  options,
  onChange,
  name,
}: {
  id: string
  label: string
  allLabel: string
  value: string[]
  options: MultiSelectOption[]
  onChange: (value: string[]) => void
  name?: string
}) {
  const trigger = useRef<HTMLButtonElement>(null)
  const [insideDialog, setInsideDialog] = useState(false)
  const [topBoundary, setTopBoundary] = useState(8)
  const selected = value.length
    ? value.map((item) => options.find((option) => option.value === item)?.label ?? item).join(', ')
    : allLabel
  const groups = [...new Set(options.map((option) => option.group ?? ''))]
  return (
    <>
      {name && value.map((item) => <input key={item} type="hidden" name={name} value={item} />)}
      <DropdownMenu.Root
        modal={false}
        onOpenChange={(open) => {
          if (!open) return
          const modal = Boolean(trigger.current?.closest('[role="dialog"], [role="alertdialog"]'))
          setInsideDialog(modal)
          const header = document.querySelector('[data-app-shell] header')
          setTopBoundary(modal ? 8 : Math.max(8, (header?.getBoundingClientRect().bottom ?? 0) + 8))
        }}
      >
        <DropdownMenu.Trigger asChild>
          <Button
            id={id}
            ref={trigger}
            aria-label={label}
            type="button"
            variant="outline"
            className="min-w-0 w-full max-w-full justify-between py-1 font-normal"
          >
            <span className="min-w-0 flex-1 truncate text-left" title={selected}>
              {selected}
            </span>
            <CaretDownIcon className="shrink-0" />
          </Button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content
            aria-label={label}
            align="start"
            sideOffset={5}
            collisionPadding={{ top: topBoundary, bottom: 8, left: 8, right: 8 }}
            className={`${insideDialog ? 'z-(--layer-dialog-menu)' : 'z-(--layer-menu)'} max-h-[min(18rem,var(--radix-dropdown-menu-content-available-height))] min-w-[var(--radix-dropdown-menu-trigger-width)] max-w-[calc(100vw-2rem)] overflow-y-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-lg`}
          >
            <DropdownMenu.Item
              onSelect={() => onChange([])}
              className="flex min-h-(--control-height) cursor-pointer items-center rounded-sm px-3 py-2 text-sm outline-none data-[highlighted]:bg-accent"
            >
              {allLabel}
            </DropdownMenu.Item>
            <DropdownMenu.Separator className="my-1 h-px bg-border" />
            {groups.map((group) => (
              <DropdownMenu.Group key={group} aria-label={group || undefined}>
                {group && (
                  <DropdownMenu.Label className="px-3 py-2 text-xs font-medium text-muted-foreground">
                    {group}
                  </DropdownMenu.Label>
                )}
                {options
                  .filter((option) => (option.group ?? '') === group)
                  .map((option) => (
                    <DropdownMenu.CheckboxItem
                      key={option.value}
                      data-value={option.value}
                      checked={value.includes(option.value)}
                      onCheckedChange={(checked) =>
                        onChange(
                          checked
                            ? [...new Set([...value, option.value])]
                            : value.filter((item) => item !== option.value),
                        )
                      }
                      onSelect={(event) => event.preventDefault()}
                      className="relative flex min-h-(--control-height) cursor-pointer items-center rounded-sm py-2 pr-3 pl-8 text-sm outline-none data-[highlighted]:bg-accent"
                    >
                      <DropdownMenu.ItemIndicator className="absolute inset-y-0 left-2 flex items-center">
                        <CheckIcon aria-hidden="true" />
                      </DropdownMenu.ItemIndicator>
                      <span className="break-words">{option.label}</span>
                    </DropdownMenu.CheckboxItem>
                  ))}
              </DropdownMenu.Group>
            ))}
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
    </>
  )
}
