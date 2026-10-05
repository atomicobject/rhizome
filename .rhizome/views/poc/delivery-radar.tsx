import { NoteLink, useGraphQL } from "@rhizome/kit";
import { Badge, Card, CardContent, CardDescription, CardHeader, CardTitle } from "@rhizome/ui";

import { Column, Loading, Page, groupBy } from "./components/Page.tsx";

type Effort = { path: string; id: string; name: string; status: string; summary: string | null };
type Spec = { path: string; specStatus: string };

const QUERY = `{
  effortNote(first: 200, sort: [{ field: "createdAt", direction: desc }]) { path id name status summary }
  technicalSpec(first: 500) { path specStatus }
  productSpec(first: 500) { path specStatus }
  experienceSpec(first: 500) { path specStatus }
}`;

type Data = { effortNote: Effort[]; technicalSpec: Spec[]; productSpec: Spec[]; experienceSpec: Spec[] };

export default function DeliveryRadar() {
  const { data, error, isPending } = useGraphQL<Data>(QUERY);
  const specs = [...(data?.technicalSpec ?? []), ...(data?.productSpec ?? []), ...(data?.experienceSpec ?? [])];

  return (
    <Page title="Delivery Radar" summary="Efforts by status beside the spec pipeline, straight from the graph.">
      {isPending && <Loading />}
      {error && <p className="text-destructive">{error.message}</p>}
      <div className="mb-4 flex gap-3">
        {groupBy(specs, (spec) => spec.specStatus).map(([status, group]) => (
          <Card key={status} className="w-40 gap-1 py-3">
            <CardHeader className="px-4">
              <CardDescription className="text-xs uppercase">{status} specs</CardDescription>
              <CardTitle className="text-2xl tabular-nums">{group.length}</CardTitle>
            </CardHeader>
          </Card>
        ))}
      </div>
      <div className="flex items-start gap-4">
        {groupBy(data?.effortNote ?? [], (effort) => effort.status).map(([status, group]) => (
          <Column key={status} title={status} count={group.length} tone={status === "active" ? "default" : "secondary"}>
            {group.map((effort) => (
              <CardContent key={effort.path} className="px-3 py-2">
                <NoteLink path={effort.path} className="text-sm font-medium hover:underline">
                  {effort.name}
                </NoteLink>
                <p className="line-clamp-2 text-xs text-muted-foreground">{effort.summary}</p>
                <Badge variant="outline" className="mt-1 font-mono">
                  {effort.id}
                </Badge>
              </CardContent>
            ))}
          </Column>
        ))}
      </div>
    </Page>
  );
}
