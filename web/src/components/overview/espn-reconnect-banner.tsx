import { Button } from "@/components/ui/button"

export function EspnReconnectBanner({
  onReconnect,
}: {
  onReconnect: () => void
}) {
  return (
    <div className="mb-6 flex flex-wrap items-center justify-between gap-3 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-[#5a4a2e] dark:bg-[#332b1f] dark:text-[#e0c48f]">
      <p role="alert">
        ESPN rejected the saved cookies, so ESPN leagues can&apos;t sync.
      </p>
      <Button variant="outline" size="sm" onClick={onReconnect}>
        Reconnect
      </Button>
    </div>
  )
}
