import { Button } from "@/components/ui/button"

export function BackButton({
  children,
  onClick,
}: {
  children: string
  onClick: () => void
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      className="w-fit px-0 text-muted-foreground"
      onClick={onClick}
    >
      {children}
    </Button>
  )
}
