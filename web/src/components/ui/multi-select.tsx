import { DropdownMenu } from 'radix-ui'
import { CheckIcon, CaretDownIcon } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'

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
  options: { value: string; label: string }[]
  onChange: (value: string[]) => void
  name?: string
}) {
  const selected = value.length
    ? value.map((item) => options.find((option) => option.value === item)?.label ?? item).join(', ')
    : allLabel
  return (
    <>
      {name && value.map((item) => <input key={item} type="hidden" name={name} value={item} />)}
      <DropdownMenu.Root modal={false}>
        <DropdownMenu.Trigger asChild>
          <Button
            id={id}
            aria-label={label}
            type="button"
            variant="outline"
            className="min-w-0 w-full max-w-full justify-between py-1 text-base md:text-sm"
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
            className="z-50 max-h-72 min-w-[var(--radix-dropdown-menu-trigger-width)] max-w-[calc(100vw-2rem)] overflow-y-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-lg"
          >
            <DropdownMenu.Item
              onSelect={() => onChange([])}
              className="cursor-pointer rounded-sm px-3 py-2 text-sm outline-none data-[highlighted]:bg-accent"
            >
              {allLabel}
            </DropdownMenu.Item>
            <DropdownMenu.Separator className="my-1 h-px bg-border" />
            {options.map((option) => (
              <DropdownMenu.CheckboxItem
                key={option.value}
                checked={value.includes(option.value)}
                onCheckedChange={(checked) =>
                  onChange(
                    checked
                      ? [...new Set([...value, option.value])]
                      : value.filter((item) => item !== option.value),
                  )
                }
                onSelect={(event) => event.preventDefault()}
                className="relative cursor-pointer rounded-sm py-2 pr-3 pl-8 text-sm outline-none data-[highlighted]:bg-accent"
              >
                <DropdownMenu.ItemIndicator className="absolute left-2 top-2.5">
                  <CheckIcon aria-hidden="true" />
                </DropdownMenu.ItemIndicator>
                <span className="break-words">{option.label}</span>
              </DropdownMenu.CheckboxItem>
            ))}
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
    </>
  )
}
