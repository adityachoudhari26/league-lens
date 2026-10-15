import type { PropsWithChildren, ReactNode } from "react"

export function InstructionSteps({ children }: PropsWithChildren) {
  return <ol className="grid gap-3">{children}</ol>
}

export function InstructionStep({
  number,
  title,
  children,
}: {
  number: number
  title: string
  children: ReactNode
}) {
  return (
    <li className="flex items-start gap-3">
      <span className="flex size-5 shrink-0 items-center justify-center rounded-full border text-[0.7rem] font-medium">
        {number}
      </span>
      <div className="min-w-0">
        <p className="font-medium text-sm">{title}</p>
        <div className="text-xs leading-5 text-muted-foreground">
          {children}
        </div>
      </div>
    </li>
  )
}
