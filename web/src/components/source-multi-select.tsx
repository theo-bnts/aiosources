import { DropdownMenu } from 'radix-ui'
import { CheckIcon, CaretDownIcon } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'

export function SourceMultiSelect({
  id,
  value,
  options,
  onChange,
}: {
  id: string
  value: string[]
  options: { value: string; label: string }[]
  onChange: (value: string[]) => void
}) {
  const label =
    value.length === 0
      ? 'All sources'
      : value.length === 1
        ? (options.find((option) => option.value === value[0])?.label ?? value[0])
        : `${value.length} sources`
  return (
    <>
      {value.map((source) => (
        <input key={source} type="hidden" name="provider" value={source} />
      ))}
      <DropdownMenu.Root modal={false}>
        <DropdownMenu.Trigger asChild>
          <Button
            id={id}
            aria-label="Sources"
            type="button"
            variant="outline"
            className="w-full justify-between"
          >
            <span className="min-w-0 truncate">{label}</span>
            <CaretDownIcon className="shrink-0" />
          </Button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content
            aria-label="Sources"
            align="start"
            sideOffset={5}
            className="z-50 max-h-72 min-w-[var(--radix-dropdown-menu-trigger-width)] max-w-[calc(100vw-2rem)] overflow-y-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-lg"
          >
            <DropdownMenu.Item
              onSelect={() => onChange([])}
              className="cursor-pointer rounded-sm px-3 py-2 text-sm outline-none data-[highlighted]:bg-accent"
            >
              All sources
            </DropdownMenu.Item>
            <DropdownMenu.Separator className="my-1 h-px bg-border" />
            {options.map((option) => (
              <DropdownMenu.CheckboxItem
                key={option.value}
                checked={value.includes(option.value)}
                onCheckedChange={(checked) =>
                  onChange(
                    checked
                      ? [...value, option.value]
                      : value.filter((source) => source !== option.value),
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
