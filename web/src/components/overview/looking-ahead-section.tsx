import { useQuery } from "@tanstack/react-query"
import { overviewQueries } from "@/lib/api/overview"
import { Section, SectionLoading, SectionError, SectionMessage } from "./section"

export function LookingAhead() {
  const query = useQuery(overviewQueries.ahead)
  return <Section title="Looking ahead">{query.isPending ? <SectionLoading /> : query.isError ? <SectionError retry={() => { void query.refetch() }} /> : <SectionMessage>{query.data.coverage?.explanation ?? "Upcoming roster checks are not available."}</SectionMessage>}</Section>
}
