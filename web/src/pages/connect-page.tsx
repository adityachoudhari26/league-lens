import { useEffect, useState } from "react"
import { Link, useNavigate } from "@tanstack/react-router"
import { ConnectDialog } from "@/components/connect/connect-dialog"

export function ConnectPage() {
  const navigate = useNavigate()
  const [credentials, setCredentials] = useState(readInitialESPNCredentials)
  const [open, setOpen] = useState(() => Boolean(credentials))

  useEffect(() => {
    document.title = "Connect leagues · League Lens"
    window.history.replaceState(null, "", "/connect")
  }, [])

  function handleOpenChange(nextOpen: boolean) {
    setOpen(nextOpen)
    if (!nextOpen) {
      setCredentials(null)
      void navigate({ to: "/settings" })
    }
  }

  return (
    <>
      <div className="mx-auto flex w-full max-w-xl flex-1 items-center justify-center">
        <section className="w-full rounded-xl border bg-card px-5 py-6 sm:px-8">
          <h1 className="text-lg font-semibold">Connect leagues</h1>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">
            {credentials
              ? "Opening your secure ESPN connection…"
              : "To connect ESPN, drag the bookmarklet from Settings → Leagues to your bookmarks bar. You can also connect a Sleeper account in Settings."}
          </p>
          {!credentials && (
            <Link
              to="/settings"
              className="mt-4 inline-flex text-sm font-medium underline underline-offset-4"
            >
              Go to Settings
            </Link>
          )}
        </section>
      </div>
      {open && credentials && (
        <ConnectDialog
          open={open}
          onOpenChange={handleOpenChange}
          initialProvider="espn"
          initialESPN={credentials}
        />
      )}
    </>
  )
}

function readInitialESPNCredentials() {
  const params = new URLSearchParams(window.location.hash.slice(1))
  const s2 = params.get("s2")
  const swid = params.get("swid")
  return s2 && swid ? { s2, swid } : null
}
