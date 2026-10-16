import type { ActionProposal } from "@/gen/leaguelens/v1/assistant_pb"
import { Button } from "@/components/ui/button"

type AssistantActionProposalProps = {
  proposal: ActionProposal
  disabled: boolean
  pending: boolean
  onConfirm: () => void
  onCancel: () => void
}

export function AssistantActionProposal({
  proposal,
  disabled,
  pending,
  onConfirm,
  onCancel,
}: AssistantActionProposalProps) {
  return (
    <div className="rounded-lg border border-border bg-muted/40 p-3">
      <p className="text-sm font-medium">{proposal.summary}</p>
      <div className="mt-3 flex gap-2">
        <Button type="button" size="sm" disabled={disabled} onClick={onConfirm}>
          {pending ? "Confirming…" : "Confirm"}
        </Button>
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={disabled}
          onClick={onCancel}
        >
          Cancel
        </Button>
      </div>
    </div>
  )
}
