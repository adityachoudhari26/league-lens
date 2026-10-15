export function InlineError({ children }: { children: string }) {
  return (
    <p
      role="alert"
      className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-[#5a4a2e] dark:bg-[#332b1f] dark:text-[#e0c48f]"
    >
      {children}
    </p>
  )
}
