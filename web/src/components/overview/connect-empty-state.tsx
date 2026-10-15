import { Button } from "@/components/ui/button"

export function ConnectEmptyState({ onConnect }: { onConnect: () => void }) {
  return (
    <section className="rounded-xl border bg-card px-5 py-8 text-center sm:px-8">
      <h2 className="text-lg font-semibold">Connect your first league</h2>
      <p className="mt-2 text-sm text-muted-foreground">
        Connect ESPN or Sleeper to bring your league into League Lens.
      </p>
      <Button className="mt-5" onClick={onConnect}>
        Connect leagues
      </Button>
    </section>
  )
}
