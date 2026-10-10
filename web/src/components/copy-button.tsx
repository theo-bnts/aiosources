import { useEffect, useRef, useState } from 'react'
import { CheckIcon, CopyIcon } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'

export function CopyButton({
  value,
  label,
  manualCopyMessage = 'Clipboard access is unavailable. Select the text and copy it manually.',
  successMessage = 'Copied to your clipboard.',
  variant = 'outline',
}: {
  value: string
  label: string
  manualCopyMessage?: string
  successMessage?: string
  variant?: 'default' | 'outline'
}) {
  const [feedback, setFeedback] = useState<'idle' | 'copied' | 'failed'>('idle')
  const [copying, setCopying] = useState(false)
  const pending = useRef(false)
  const generation = useRef(0)
  useEffect(() => {
    generation.current++
    setFeedback('idle')
    return () => {
      generation.current++
    }
  }, [value])
  async function copy() {
    if (pending.current) return
    pending.current = true
    setCopying(true)
    setFeedback('idle')
    const attempt = generation.current
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable')
      await navigator.clipboard.writeText(value)
      if (generation.current === attempt) setFeedback('copied')
    } catch {
      if (generation.current === attempt) setFeedback('failed')
    } finally {
      pending.current = false
      setCopying(false)
    }
  }
  return (
    <div className="space-y-2">
      <Button
        type="button"
        variant={variant}
        aria-label={label}
        disabled={copying || !value}
        onClick={() => void copy()}
      >
        {feedback === 'copied' ? <CheckIcon /> : <CopyIcon />}
        {copying ? 'Copying…' : feedback === 'copied' ? 'Copied' : label}
      </Button>
      {feedback === 'copied' && (
        <p className="help" role="status">
          {successMessage}
        </p>
      )}
      {feedback === 'failed' && (
        <p className="help text-destructive" role="alert">
          {manualCopyMessage}
        </p>
      )}
    </div>
  )
}
