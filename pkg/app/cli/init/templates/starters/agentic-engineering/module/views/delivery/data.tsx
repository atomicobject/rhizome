import { useQuery, useQueryClient } from '@tanstack/react-query';
import { graphql, readNodes, responseGeneration, typedRootCoverage, useViewActive, markdownChecklist } from '@rhizome/kit';
import { reader, type ReadAPI } from './reads.ts';
import { identity, type Coverage, type Ref } from './lib/index.ts';
export type { LiveNode, FieldState, LiveSpec, LiveStory, LiveCriterion, LiveEffort, RecordDetail, PortfolioRead } from './queries.ts';

const api: ReadAPI = { graphql, readNodes, generation: responseGeneration, roots: typedRootCoverage, markdownChecklist };

export function useDeliveryPortfolio(options: { enabled?: boolean; review?: boolean } = {}) {
  const active = useViewActive();
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ['agentic-engineering', 'portfolio', options.review ?? false],
    queryFn: ({ signal }) => reader(api, signal).portfolio(options.review),
    enabled: active && options.enabled !== false,
    subscribed: active,
    retry: false,
  });
  return { ...query, state: query.isPending ? 'loading' : query.isError ? 'error'
    : query.data?.coverage.complete ? 'ready' : 'partial', refresh: () => {
      void queryClient.invalidateQueries({ queryKey: ['agentic-engineering', 'selection'] });
      return query.refetch();
    },
    retained: query.isError && query.data !== undefined, mode: 'committed' as const };
}

export function useDeliveryRecord(ref: Ref | null) {
  const active = useViewActive();
  const query = useQuery({
    queryKey: ['agentic-engineering', 'record', ref ? identity(ref) : null],
    queryFn: ({ signal }) => ref ? reader(api, signal).record(ref) : Promise.resolve(null),
    enabled: active && ref !== null,
    subscribed: active,
    retry: false,
  });
  return { ...query, state: !ref ? 'invalid-route' : query.isPending ? 'loading' : query.isError ? 'error'
    : query.data === null ? 'missing-node' : query.data?.coverage.complete ? 'ready' : 'partial',
    refresh: query.refetch, retained: query.isError && query.data !== undefined, mode: 'committed' as const };
}

/** Only selected effort rows load their execution scope for prompt composition. */
export function useDeliverySelectedEfforts(refs: readonly Ref[], generation: Coverage['graphGeneration']) {
  const active = useViewActive();
  return useQuery({
    queryKey: ['agentic-engineering', 'selection', refs.map(identity), generation],
    queryFn: ({ signal }) => reader(api, signal).selectedEfforts(refs),
    enabled: active && refs.length > 0,
    subscribed: active,
    retry: false,
  });
}
