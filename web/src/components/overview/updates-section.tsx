import { useQuery } from "@tanstack/react-query"
import { overviewQueries } from "@/lib/api/overview"
import {
  Section,
  SectionLoading,
  SectionError,
  SectionMessage,
} from "./section"

export function UpdatesSection() {
  const query = useQuery(overviewQueries.updates)
  return (
    <Section title="Player updates">
      {query.isPending ? (
        <SectionLoading />
      ) : query.isError ? (
        <SectionError
          retry={() => {
            void query.refetch()
          }}
        />
      ) : (
        <SectionMessage>
          {query.data.coverage?.explanation ?? "Player news is not available."}
        </SectionMessage>
      )}
    </Section>
  )
}
