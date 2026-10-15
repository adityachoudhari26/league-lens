import { Bookmark, Check, Copy } from "lucide-react"
import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"

function bookmarkletCode(origin: string) {
  const quotedOrigin = JSON.stringify(origin)
  return `javascript:(()=>{if(!/(^|\\.)espn\\.com$/.test(location.hostname)){alert('Open espn.com while logged in, then click this bookmark.');return}const c={};document.cookie.split('; ').forEach(p=>{const i=p.indexOf('=');c[p.slice(0,i)]=p.slice(i+1)});if(!c.espn_s2||!c.SWID){alert('Log in to ESPN first.');return}const e=v=>encodeURIComponent(decodeURIComponent(v));window.open(${quotedOrigin}+'/connect#s2='+e(c.espn_s2)+'&swid='+e(c.SWID))})()`
}

const isMac = /Mac|iPhone|iPad/.test(navigator.platform)
const bookmarksBarShortcut = isMac ? "⌘⇧B" : "Ctrl+Shift+B"

export function EspnBookmarklet() {
  const linkRef = useRef<HTMLAnchorElement>(null)
  const [clickedInApp, setClickedInApp] = useState(false)
  const [copied, setCopied] = useState(false)
  const code = bookmarkletCode(window.location.origin)

  // React 19 rewrites javascript: URLs in JSX, so set the href directly.
  useEffect(() => {
    linkRef.current?.setAttribute("href", code)
  }, [code])

  async function copyCode() {
    await navigator.clipboard.writeText(code)
    setCopied(true)
  }

  return (
    <section className="rounded-lg border bg-muted/30 px-4 py-3">
      <h3 className="text-sm font-medium">Connect with a bookmark</h3>
      <ol className="mt-2 list-decimal space-y-1 pl-5 text-xs leading-5 text-muted-foreground">
        <li>Show your bookmarks bar ({bookmarksBarShortcut}).</li>
        <li>Drag the button below onto the bookmarks bar.</li>
        <li>
          Open ESPN Fantasy Football while logged in and click the new bookmark.
          League Lens opens with your leagues.
        </li>
      </ol>
      <a
        ref={linkRef}
        href="https://www.espn.com"
        draggable
        onClick={(event) => {
          event.preventDefault()
          setClickedInApp(true)
        }}
        className="mt-3 inline-flex cursor-grab items-center gap-2 rounded-md border bg-background px-3 py-1.5 text-sm font-medium shadow-xs focus-visible:outline-2 focus-visible:outline-ring active:cursor-grabbing"
      >
        <Bookmark className="size-4" aria-hidden />
        Connect to League Lens
      </a>
      {clickedInApp && (
        <p role="status" className="mt-2 text-xs text-muted-foreground">
          Drag this button to your bookmarks bar instead of clicking it here.
        </p>
      )}
      <div className="mt-3 border-t pt-3 text-xs leading-5 text-muted-foreground">
        <p>
          Can&apos;t drag it? Copy the code, add a new bookmark, and paste the
          code as its URL. Pasting it into the address bar doesn&apos;t work.
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
