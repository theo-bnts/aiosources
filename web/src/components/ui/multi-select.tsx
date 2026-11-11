import { DropdownMenu } from 'radix-ui'
import { useEffect, useRef, useState } from 'react'
import { CheckIcon, CaretDownIcon, MinusIcon } from '@phosphor-icons/react'
import { popupLayer } from '@/lib/popup-layer'
import { Button } from '@/components/ui/button'
import {
  selectionGroupState,
  selectionLabels,
  toggleSelectionGroup,
  type MultiSelectOption,
} from '@/lib/multi-select-values'

export type { MultiSelectOption } from '@/lib/multi-select-values'

// The caller defines the empty selection. Source selectors use all sources,
// including future entries; optional-field selectors use required fields only.
export function MultiSelect({
  id,
  label,
  allLabel,
  value,
  options,
  onChange,
  name,
  disabled = false,
  selectGroups = false,
}: {
  id: string
  label: string
  allLabel: string
  value: string[]
  options: MultiSelectOption[]
  onChange: (value: string[]) => void
  name?: string
  disabled?: boolean
  selectGroups?: boolean
}) {
  const trigger = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState(false)
  useEffect(() => {
    if (disabled) setOpen(false)
  }, [disabled])
  const [insideDialog, setInsideDialog] = useState(false)
  const [topBoundary, setTopBoundary] = useState(8)
  const selected = value.length ? selectionLabels(value, options, selectGroups) : allLabel
  const groups = [...new Set(options.map((option) => option.group ?? ''))]
  return (
    <>
      {name &&
        value.map((item) => (
          <input key={item} type="hidden" name={name} value={item} disabled={disabled} />
        ))}
      <DropdownMenu.Root
        modal={false}
        open={!disabled && open}
        onOpenChange={(open) => {
          setOpen(open && !disabled)
          if (!open) return
          const layer = popupLayer(trigger.current)
          setInsideDialog(layer.dialog)
          setTopBoundary(layer.top)
        }}
      >
        <DropdownMenu.Trigger asChild>
          <Button
            id={id}
            ref={trigger}
            aria-label={label}
            type="button"
            disabled={disabled}
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
            {groups.map((group) => {
              const members = options.filter((option) => (option.group ?? '') === group)
              const parent =
                !!group && (!selectGroups || members.length !== 1 || members[0].label !== group)
              const checked = selectionGroupState(value, members)
              return (
                <DropdownMenu.Group key={group} aria-label={group || undefined}>
                  {parent && selectGroups ? (
                    <DropdownMenu.CheckboxItem
                      data-group={group}
                      checked={checked}
                      onCheckedChange={(next) =>
                        onChange(toggleSelectionGroup(value, members, next === true))
                      }
                      onSelect={(event) => event.preventDefault()}
                      className="relative flex min-h-(--control-height) cursor-pointer items-center rounded-sm py-2 pr-3 pl-8 text-sm font-medium outline-none data-[highlighted]:bg-accent"
                    >
                      <DropdownMenu.ItemIndicator className="absolute inset-y-0 left-2 flex items-center">
                        {checked === 'indeterminate' ? (
                          <MinusIcon aria-hidden="true" />
                        ) : (
                          <CheckIcon aria-hidden="true" />
                        )}
                      </DropdownMenu.ItemIndicator>
                      <span>{group}</span>
                    </DropdownMenu.CheckboxItem>
                  ) : parent ? (
                    <DropdownMenu.Label className="px-3 py-2 text-xs font-medium text-muted-foreground">
                      {group}
                    </DropdownMenu.Label>
                  ) : null}
                  {members.map((option) => (
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
                      className={`relative flex min-h-(--control-height) cursor-pointer items-center rounded-sm py-2 pr-3 ${parent && selectGroups ? 'pl-12' : 'pl-8'} text-sm outline-none data-[highlighted]:bg-accent`}
                    >
                      <DropdownMenu.ItemIndicator
                        className={`absolute inset-y-0 ${parent && selectGroups ? 'left-6' : 'left-2'} flex items-center`}
                      >
                        <CheckIcon aria-hidden="true" />
                      </DropdownMenu.ItemIndicator>
                      <span className="break-words">{option.label}</span>
                    </DropdownMenu.CheckboxItem>
                  ))}
                </DropdownMenu.Group>
              )
            })}
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
    </>
  )
}
