import { GearSixIcon } from '@phosphor-icons/react'
import {
  useBrowserPreferences,
  saveBrowserPreferences,
  type BrowserPreferences,
} from '@/lib/browser-preferences'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'

export function InterfacePreferences() {
  const preferences = useBrowserPreferences()
  function change<K extends keyof BrowserPreferences>(key: K, value: BrowserPreferences[K]) {
    saveBrowserPreferences({ ...preferences, [key]: value })
  }
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button
          variant="outline"
          size="icon"
          className="size-11 bg-background lg:size-9 dark:bg-background"
          aria-label="Interface preferences"
          title="Interface preferences"
        >
          <GearSixIcon />
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Interface preferences</DialogTitle>
          <DialogDescription>
            Applies throughout the interface on this browser. Changes are applied automatically.
          </DialogDescription>
        </DialogHeader>
        <div className="grid items-start gap-5 sm:grid-cols-2">
          <div className="min-w-0 space-y-2">
            <Label htmlFor="interface-page-size">Rows per page</Label>
            <select
              id="interface-page-size"
              className="native-select w-full"
              value={preferences.pageSize}
              onChange={(event) =>
                change('pageSize', Number(event.target.value) as BrowserPreferences['pageSize'])
              }
            >
              {[25, 50, 100].map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </select>
          </div>
          <div className="min-w-0 space-y-2">
            <Label htmlFor="interface-refresh">Automatic refresh</Label>
            <select
              id="interface-refresh"
              className="native-select w-full"
              value={preferences.refresh}
              onChange={(event) =>
                change('refresh', event.target.value as BrowserPreferences['refresh'])
              }
            >
              <option value="normal">Normal</option>
              <option value="reduced">Reduced activity</option>
            </select>
          </div>
          <div className="min-w-0 space-y-2">
            <Label htmlFor="interface-date-format">Date format</Label>
            <select
              id="interface-date-format"
              className="native-select w-full"
              value={preferences.dateFormat}
              onChange={(event) =>
                change('dateFormat', event.target.value as BrowserPreferences['dateFormat'])
              }
            >
              <option value="browser">Browser language</option>
              <option value="english">English (UK)</option>
              <option value="iso">YYYY-MM-DD</option>
            </select>
          </div>
          <div className="min-w-0 space-y-2">
            <Label htmlFor="interface-time-zone">Time zone</Label>
            <select
              id="interface-time-zone"
              className="native-select w-full"
              value={preferences.timeZone}
              onChange={(event) =>
                change('timeZone', event.target.value as BrowserPreferences['timeZone'])
              }
            >
              <option value="browser">Browser time zone</option>
              <option value="utc">UTC</option>
            </select>
          </div>
        </div>
        <p className="help">
          Reduced activity refreshes lists and progress three times less often. Server schedules and
          background work are unchanged.
        </p>
      </DialogContent>
    </Dialog>
  )
}
