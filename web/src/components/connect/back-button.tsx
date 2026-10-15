import { ArrowLeft } from "lucide-react"
import { Button } from "@/components/ui/button"

export function BackButton({
  label,
  onClick,
}: {
  label: string
  onClick: () => void
}) {
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      className="w-fit"
      aria-label={label}
      onClick={onClick}
    >
      <ArrowLeft aria-hidden />
      Back
    </Button>
  )
}
