import { Bookmark, Check, Copy } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { bookmarkletCode } from "./bookmarklet"
import { InstructionStep, InstructionSteps } from "./instruction-steps"
import { isMac } from "./platform"

const bookmarksBarShortcut = isMac ? "⌘⇧B" : "Ctrl+Shift+B"

export function EspnBookmarkletOption() {
  const linkRef = useRef<HTMLAnchorElement>(null)
  const [clickedInApp, setClickedInApp] = useState(false)
  const [copied, setCopied] = useState(false)
  const code = bookmarkletCode(window.location.origin)

  useEffect(() => {
    // React rewrites javascript: links in JSX, so set the bookmark URL directly.
    linkRef.current?.setAttribute("href", code)
  }, [code])

  async function copyCode() {
    await navigator.clipboard.writeText(code)
    setCopied(true)
  }

  return (
    <section className="grid gap-3 rounded-lg border bg-muted/30 px-4 py-3">
      <h3 className="text-sm font-medium">
        Option 1: Use a bookmark (recommended)
      </h3>
      <InstructionSteps>
        <InstructionStep number={1} title="Show your bookmarks bar">
          Press {bookmarksBarShortcut} if it&apos;s hidden.
        </InstructionStep>
        <InstructionStep
          number={2}
          title="Drag this button onto the bookmarks bar"
        >
          <a
            ref={linkRef}
            href="https://www.espn.com"
            draggable
            onClick={(event) => {
              event.preventDefault()
              setClickedInApp(true)
            }}
            className="mt-1 inline-flex cursor-grab items-center gap-2 rounded-md border bg-background px-3 py-1.5 text-sm font-medium text-foreground shadow-xs focus-visible:outline-2 focus-visible:outline-ring active:cursor-grabbing"
          >
            <Bookmark className="size-4" aria-hidden />
            Connect to League Lens
          </a>
          <p role="status" className="mt-1">
            {clickedInApp
              ? "Drag this button to your bookmarks bar instead of clicking it here."
              : "Drag it. Clicking it here does nothing."}
          </p>
        </InstructionStep>
        <InstructionStep
          number={3}
          title="Open ESPN Fantasy Football and sign in"
        >
          <a
            href="https://www.espn.com/fantasy/"
            target="_blank"
            rel="noreferrer"
            className="underline underline-offset-2"
          >
            espn.com/fantasy
          </a>
        </InstructionStep>
        <InstructionStep number={4} title="Click the bookmark on the ESPN tab">
          League Lens opens in a new tab with your leagues ready to import. Next
          time ESPN asks you to reconnect, just click the bookmark again.
        </InstructionStep>
      </InstructionSteps>
      <div className="border-t pt-3 text-xs leading-5 text-muted-foreground">
        <p>
          Can&apos;t drag it? Copy the code, add a new bookmark, and paste the
          code as its URL. Pasting it into the address bar won&apos;t work.
        </p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-2"
          onClick={() => void copyCode()}
        >
          {copied ? (
            <Check className="size-4" aria-hidden />
          ) : (
            <Copy className="size-4" aria-hidden />
          )}
          {copied ? "Copied" : "Copy bookmark code"}
        </Button>
      </div>
    </section>
  )
}
