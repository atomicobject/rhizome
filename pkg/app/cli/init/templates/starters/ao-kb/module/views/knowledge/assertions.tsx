import { Count, Section, StatusMark, sectionStatus, setViewParams } from "@rhizome/kit";
import { COVERAGE, filterAssertions, name } from "./lib/knowledge.ts";
import type { Assertion, Knowledge } from "./lib/knowledge.ts";
import type { Edge } from "./lib/relations.ts";
import { useCoverage } from "./data.tsx";
import { DateValue, Empty, Filters, Page, Record, StatusBadges, useFilters } from "./parts.tsx";

const columns = [
  ["title", "Assertion"],
  ["kind", "Kind"],
  ["confidence", "Stored confidence"],
  ["status", "Status"],
  ["sources", "Sources / citation state"],
  ["updatedAt", "Updated"],
  ["priority", "Priority"],
  ["horizon", "Horizon"],
];
function Table({ data, edges }: { data: Knowledge; edges: Edge[] }) {
  const filters = useFilters(),
    citations = useCoverage(data);
  const [key, direction] = filters.sort.split("-");
  const value = (a: Assertion) => {
    switch (key) {
      case "kind":
        return a.kind || "";
      case "confidence":
        return a.confidence || "";
      case "status":
        return a.status || "";
      case "sources":
        return citations.data?.get(a.path) || "";
      case "updatedAt":
        return a.updatedAt || "";
      case "priority":
        return a.priority || "";
      case "horizon":
        return a.horizon || "";
      default:
        return name(a);
    }
  };
  const rows = filterAssertions(data.assertion, filters.find, filters.concept).sort(
    (a, b) =>
      String(value(a)).localeCompare(String(value(b))) * (direction === "desc" ? -1 : 1) ||
      a.path.localeCompare(b.path),
  );
  return (
    <>
      <Filters data={data} />
      <Section
        title="Assertions"
        count={rows.length}
        caption="Stored AO facts. Locator coverage describes the origin citation, not truth or strength."
        status={sectionStatus(citations)}
        what="citation states"
      >
        {() =>
          rows.length ? (
            <div className="ak-table-wrap">
              <table className="ak-table">
                <thead>
                  <tr>
                    {columns.map(([field, label]) => (
                      <th
                        key={field}
                        scope="col"
                        aria-sort={
                          key === field
                            ? direction === "desc"
                              ? "descending"
                              : "ascending"
                            : "none"
                        }
                      >
                        <button
                          onClick={() =>
                            setViewParams({
                              sort: `${field}-${key === field && direction !== "desc" ? "desc" : "asc"}`,
                            })
                          }
                        >
                          {label}
                          {key === field ? (direction === "desc" ? " ↓" : " ↑") : ""}
                        </button>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {rows.map((a) => (
                    <tr key={a.path}>
                      <td>
                        <Record record={a} />
                        <StatusBadges a={a} edges={edges} />
                        <span className="ak-meta">
                          {a.concepts.map(name).join(" · ") || "No concepts recorded"}
                        </span>
                      </td>
                      <td>{a.kind || "Not recorded"}</td>
                      <td>{a.confidence || "Not recorded"}</td>
                      <td>
                        {a.status ? <StatusMark value={a.status} values={[]} /> : "Not recorded"}
                      </td>
                      <td>
                        <Count>{a.sourcesSection?.citations.length || 0}</Count> Sources{" "}
                        {a.sourcesSection?.citations.length === 1 ? "link" : "links"}
                        <span className="ak-meta">
                          {citations.data?.get(a.path) || "Not recorded"}
                        </span>
                      </td>
                      <td>
                        <DateValue value={a.updatedAt} />
                      </td>
                      <td>{a.priority || "Not recorded"}</td>
                      <td>{a.horizon || "Not recorded"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <Empty />
          )
        }
      </Section>
      <p className="ak-footnote">
        Coverage states: {COVERAGE.join(" · ")}. Stored confidence is starting confidence set at
        extraction; not a verdict.
      </p>
    </>
  );
}
export default function Assertions() {
  return <Page>{({ data, graph }) => <Table data={data} edges={graph?.edges || []} />}</Page>;
}
