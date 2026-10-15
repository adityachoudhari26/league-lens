import { useEffect, useRef } from "react"

export function EspnBookmarklet() {
  const linkRef = useRef<HTMLAnchorElement>(null)

  useEffect(() => {
    const link = linkRef.current
    if (!link) return
    const origin = JSON.stringify(window.location.origin) ?? '""'
    const code = `javascript:(()=>{if(!/(^|\\.)espn\\.com$/.test(location.hostname)){alert('Open espn.com while logged in, then click this bookmark.');return}const c={};document.cookie.split('; ').forEach(p=>{const i=p.indexOf('=');c[p.slice(0,i)]=p.slice(i+1)});if(!c.espn_s2||!c.SWID){alert('Log in to ESPN first.');return}const e=v=>encodeURIComponent(decodeURIComponent(v));window.open(${origin}+'/connect#s2='+e(c.espn_s2)+'&swid='+e(c.SWID))})()`
    link.setAttribute("href", code)
    const preventNavigation = (event: MouseEvent) => event.preventDefault()
    link.addEventListener("click", preventNavigation)
    return () => link.removeEventListener("click", preventNavigation)
  }, [])

  return (
    <section className="rounded-lg border bg-muted/30 px-4 py-3">
      <h3 className="text-sm font-medium">Connect with a bookmark</h3>
      <p className="mt-1 text-xs leading-5 text-muted-foreground">
        Drag this to your bookmarks bar, open espn.com while logged in, and
        click it.
      </p>
      <a
        ref={linkRef}
        href="https://www.espn.com"
        className="mt-3 inline-flex rounded-md border bg-background px-3 py-1.5 text-sm font-medium underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-ring"
      >
        Connect to League Lens
      </a>
    </section>
  )
}
