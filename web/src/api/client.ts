import { PUBLIC_LOCAL_GRAPH_QUERY, PUBLIC_NODE_DETAIL_QUERY } from "./graphql/operations";
import { nodeWorkspaceFromPublicGraphQL } from "./nodeWorkspaceAdapter";
import type { JsonObject } from "./parse";
import { isCallable, isErrorResponse, isString } from "./parse";
import type {
  PublicGraphQLResult,
  PublicLocalGraphData,
  PublicNodeDetailData,
} from "./publicGraphQLTypes";
import type {
  AgentApprovalDecision,
  AgentSendMessageResponse,
  AgentSessionResponse,
  AgentSessionsResponse,
  AgentSettings,
  AgentSettingsResponse,
  EditSessionReadRequest,
  FileView,
  GraphResponse,
  ModifiedNotesResponse,
  NodePreview,
  NodeRef,
  NodeWorkspace,
  NodeWorkspaceGraph,
  NoteSearchResponse,
  OntologyAtlasResponse,
  OntologyEditOp,
  OntologyEditSessionResponse,
  OntologyEditSessionSnapshot,
  OntologyInspectResponse,
  OntologyNoteListItem,
  OntologyQueryReadRequest,
  OntologyQueryRequest,
  OntologyQueryResult,
  OntologyQuerySchemaResponse,
  OntologySummaryResponse,
  OntologyTypeResponse,
  RenderedFile,
  SearchResponse,
  WorkspaceSearchResponse,
  StatusResponse,
  SuggestResponse,
  TreeResponse,
  ValidateEnvelope,
  ValidationDiagnosticPage,
  ValidationDiagnosticsParams,
  ValidationIssueGroupRequest,
  ValidationIssueGroupResponse,
  ValidationRepairApplyResponse,
  ValidationRepairReview,
  ValidationRepairReviewApplyRequest,
  ValidationRepairReviewCreateRequest,
  ValidationScopeSummaryRequest,
  ValidationScopeSummaryResponse,
  ViewCatalog,
  ViewCatalogEntry,
  ViewExecuteReadRequest,
  ViewExecuteRequest,
  ViewExecuteResponse,
  ViewFieldCandidatesResponse,
  ViewSaveRequest,
  ViewSaveResponse,
} from "./types";

type PublicReadOptions = RequestOptions & {
  editSession?: OntologyEditSessionResponse | OntologyEditSessionSnapshot | null;
};

export type RequestOptions = {
  signal?: AbortSignal;
};

function requestInit(options?: RequestOptions): RequestInit | undefined {
  return options?.signal ? { signal: options.signal } : undefined;
}

function activeEditSessionSnapshot(
  editSession?: OntologyEditSessionResponse | OntologyEditSessionSnapshot | null,
): OntologyEditSessionSnapshot | undefined {
  // A session the server has not acknowledged yet (no revision) exists only
  // locally; its snapshot lacks verified bases, so reads use committed state.
  // A conflicted session still reads staged state while the conflict waits.
  if (!editSession?.sessionId || !editSession.revision) return undefined;

  if ("hasUncommittedChanges" in editSession && editSession.hasUncommittedChanges === false) {
    return undefined;
  }

  return {
    version: 3,
    revision: editSession.revision || 0,
    sessionId: editSession.sessionId,
    ops: editSession.ops || [],
    baseFingerprints: editSession.baseFingerprints || {},
    baseDocuments: editSession.baseDocuments || [],
  };
}

function withEditSessionSnapshot<T extends object>(
  body: T,
  options?: PublicReadOptions,
): T & { editSession?: EditSessionReadRequest } {
  const snapshot = activeEditSessionSnapshot(options?.editSession);

  if (!snapshot) return body;

  return {
    ...body,
    editSession: {
      sessionId: snapshot.sessionId,
      snapshot,
    },
  };
}

/**
 * A read route with a staged form: GET reads committed state, and POST with
 * the same query parameters carries the edit session snapshot, which only a
 * body can hold.
 */
function stagedRead<T>(url: string, options: PublicReadOptions = {}) {
  const body = withEditSessionSnapshot({}, options);

  if (!body.editSession) return fetchJSON<T>(url, requestInit(options));

  return postJSON<typeof body, T>(url, body, options, true);
}

export class ApiError extends Error {
  readonly status: number;
  readonly url: string;
  readonly code?: string;
  readonly details?: unknown;

  constructor(
    message: string,
    options: {
      status: number;
      url: string;
      code?: string;
      details?: unknown;
    },
  ) {
    super(message);
    this.name = "ApiError";
    this.status = options.status;
    this.url = options.url;
    this.code = options.code;
    this.details = options.details;
  }
}

// The index gate returns 503 + Retry-After while first-launch indexing is in
// progress. Retry once, but keep the delay bounded and abortable so route
// changes/unmounts do not leave a stale request sleeping in the background.
const FETCH_JSON_RETRY_AFTER_CAP_SECONDS = 10;

const INDEX_INITIALIZING_CODE = "INDEX_INITIALIZING";

export function fetchJSON<T>(url: string, init?: RequestInit, retryRead = false): Promise<T> {
  return fetchWithRetryAfter(url, init, retryRead).then(async (res) => {
    if (!res.ok) {
      throw await apiErrorFromResponse(res, url, init?.signal);
    }

    return parseJSONResponse<T>(res, url, init?.signal);
  });
}

async function fetchWithRetryAfter(
  url: string,
  init?: RequestInit,
  retryRead = false,
): Promise<Response> {
  const res = await fetch(url, init);

  if (res.status !== 503) return res;

  const method = (init?.method || "GET").toUpperCase();

  if (!retryRead && method !== "GET" && method !== "HEAD") return res;

  const retryAfter = parseRetryAfterSeconds(res.headers?.get?.("retry-after"));

  if (
    retryAfter == null ||
    retryAfter > FETCH_JSON_RETRY_AFTER_CAP_SECONDS ||
    !(await isIndexInitializingResponse(res))
  ) {
    return res;
  }

  await drainBody(res, init?.signal);
  await delay(retryAfter * 1000, init?.signal ?? undefined);

  return fetch(url, init);
}

async function isIndexInitializingResponse(res: Response): Promise<boolean> {
  if (!isCallable(res.clone)) return false;
  const clone = res.clone();
  const contentType = clone.headers?.get?.("content-type") || "";

  if (!contentType.toLowerCase().includes("application/json")) return false;

  try {
    const body: unknown = await clone.json();

    return isErrorResponse(body) && body.code === INDEX_INITIALIZING_CODE;
  } catch {
    return false;
  }
}

function parseRetryAfterSeconds(raw: string | null | undefined): number | null {
  if (!raw) return null;
  const trimmed = raw.trim();

  if (trimmed === "") return null;
  const numeric = Number(trimmed);

  if (Number.isFinite(numeric) && numeric >= 0) return numeric;
  const dateMs = Date.parse(trimmed);

  if (Number.isNaN(dateMs)) return null;

  return Math.max(0, Math.round((dateMs - Date.now()) / 1000));
}

function delay(ms: number, signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) {
    return Promise.reject(signal.reason ?? new DOMException("Aborted", "AbortError"));
  }

  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);

    const onAbort = () => {
      clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
      reject(signal?.reason ?? new DOMException("Aborted", "AbortError"));
    };

    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

async function parseJSONResponse<T>(
  res: Response,
  url: string,
  signal?: AbortSignal | null,
): Promise<T> {
  const contentType = res.headers?.get?.("content-type") || "";

  if (contentType && !contentType.toLowerCase().includes("application/json")) {
    await drainBody(res, signal);
    throw new Error(
      `Expected JSON from ${url}, but received ${contentType}. The API route may be unavailable or misrouted.`,
    );
  }

  try {
    // SAFETY: the response declared `application/json` (or no content type) and
    // `res.json()` parsed it, so the value is JSON. `T` is the route's response
    // schema, supplied by the typed wrapper for that route in this module; this
    // is the single place raw JSON becomes a typed API response.
    return (await res.json()) as T;
  } catch (error) {
    signal?.throwIfAborted();

    if (error instanceof DOMException && error.name === "AbortError") throw error;
    throw new Error(`Invalid JSON response from ${url}`);
  }
}

async function drainBody(res: Response, signal?: AbortSignal | null): Promise<void> {
  try {
    await res.text();
  } catch (error) {
    signal?.throwIfAborted();

    if (error instanceof DOMException && error.name === "AbortError") throw error;
    // The response is already unusable for the UI; keep the surfaced error stable.
  }
}

async function apiErrorFromResponse(res: Response, url: string, signal?: AbortSignal | null) {
  const fallback = `Request failed: ${res.status}`;
  const contentType = res.headers?.get?.("content-type") || "";

  if (contentType.toLowerCase().includes("application/json")) {
    try {
      const body: unknown = await res.json();

      if (!isErrorResponse(body)) {
        return new ApiError(fallback, { status: res.status, url });
      }

      return new ApiError(body.error.trim() ? body.error : fallback, {
        status: res.status,
        url,
        code: body.code,
        details: body.details,
      });
    } catch (error) {
      signal?.throwIfAborted();

      if (error instanceof DOMException && error.name === "AbortError") throw error;

      return new ApiError(`Invalid JSON error response from ${url}`, {
        status: res.status,
        url,
      });
    }
  }

  await drainBody(res, signal);

  return new ApiError(fallback, { status: res.status, url });
}

function postJSON<TRequest extends object, TResponse>(
  url: string,
  body: TRequest,
  options: RequestOptions = {},
  retryRead = false,
) {
  return fetchJSON<TResponse>(
    url,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(body),
      signal: options.signal,
    },
    retryRead,
  );
}

async function postJSONAllowConflict<TRequest extends object, TResponse>(
  url: string,
  body: TRequest,
): Promise<TResponse> {
  const res = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
  });

  if (!res.ok && res.status !== 409) {
    throw await apiErrorFromResponse(res, url);
  }

  return parseJSONResponse<TResponse>(res, url);
}

// Graph endpoints may return `nodes: null` / `edges: null` when the scope has
// no data (e.g. an isolated note). Downstream consumers (useSigmaGraph) call
// `.forEach` directly, so normalize once at the boundary rather than sprinkling
// null-guards through the graph code.
function normalizeGraph(data: GraphResponse): GraphResponse {
  return {
    ...data,
    nodes: data.nodes || [],
    edges: data.edges || [],
  };
}

export function getGlobalGraph(
  options: { notesOnly?: boolean; diagnostics?: boolean } = {},
  requestOptions: RequestOptions = {},
) {
  const params = new URLSearchParams();

  if (options.notesOnly) params.set("notesOnly", "true");

  if (options.diagnostics) params.set("diagnostics", "true");
  const suffix = params.size > 0 ? `?${params.toString()}` : "";

  return fetchJSON<GraphResponse>(
    `/api/v1/graphs/global${suffix}`,
    requestInit(requestOptions),
  ).then(normalizeGraph);
}

export function getLocalGraph(
  path: string | NodeRef,
  limit: number = 200,
  options: { notesOnly?: boolean; diagnostics?: boolean } = {},
  requestOptions: RequestOptions = {},
) {
  const params = new URLSearchParams({ limit: String(limit) });

  if (!isString(path)) {
    const ref = path.fragment ? `${path.notePath}#${path.fragment}` : path.notePath;
    params.set("ref", ref);

    if (path.kind) params.set("kind", path.kind);

    if (path.nodeId) params.set("nodeId", path.nodeId);

    if (path.structuralFingerprint) params.set("structural", path.structuralFingerprint);
  } else if (path.includes("#")) {
    params.set("ref", path);
    const fragment = path.split("#", 2)[1] || "";

    if (fragment.startsWith("^")) params.set("kind", "EMBEDDED");
  } else {
    params.set("path", path);
  }

  if (options.notesOnly) params.set("notesOnly", "true");

  if (options.diagnostics) params.set("diagnostics", "true");

  return fetchJSON<GraphResponse>(
    `/api/v1/graphs/local?${params.toString()}`,
    requestInit(requestOptions),
  ).then(normalizeGraph);
}

export function getExpandedModuleGraph(
  modulePath: string,
  limit: number = 600,
  requestOptions: RequestOptions = {},
) {
  return fetchJSON<GraphResponse>(
    `/api/v1/graphs/expand?module=${encodeURIComponent(modulePath)}&limit=${limit}`,
    requestInit(requestOptions),
  ).then(normalizeGraph);
}

export function getStatus(options: RequestOptions = {}) {
  return fetchJSON<StatusResponse>("/api/v1/status", requestInit(options));
}

export function refreshPublicValidation() {
  return fetchJSON<{ accepted: boolean }>("/api/v2/validate/refresh", { method: "POST" });
}

export function getPublicValidate(options: RequestOptions = {}) {
  return fetchJSON<ValidateEnvelope>("/api/v2/validate", requestInit(options));
}

export function getPublicValidationDiagnostics(
  query: ValidationDiagnosticsParams,
  options: RequestOptions = {},
) {
  const params = new URLSearchParams();
  params.set("generation", String(query.generation));

  if (query.limit !== undefined) params.set("limit", String(query.limit));

  if (query.cursor) params.set("cursor", query.cursor);

  if (query.check) params.set("check", query.check);

  if (query.code) params.set("code", query.code);

  if (query.variant) params.set("variant", query.variant);

  if (query.path) params.set("path", query.path);

  if (query.text) params.set("text", query.text);

  if (query.scopeKind) params.set("scopeKind", query.scopeKind);

  if (query.scopeKey) params.set("scopeKey", query.scopeKey);

  if (query.repairAvailability) params.set("repairAvailability", query.repairAvailability);

  if (query.sort) params.set("sort", query.sort);

  return fetchJSON<ValidationDiagnosticPage>(
    `/api/v1/validation/diagnostics?${params.toString()}`,
    requestInit(options),
  ).then((page) => ({ ...page, diagnostics: page.diagnostics ?? [] }));
}

export function getPublicValidationScopeSummaries(
  request: ValidationScopeSummaryRequest,
  options: RequestOptions = {},
) {
  return postJSON<ValidationScopeSummaryRequest, ValidationScopeSummaryResponse>(
    "/api/v1/validation/summaries",
    request,
    options,
  );
}

export function getPublicValidationIssueGroups(
  request: ValidationIssueGroupRequest,
  options: RequestOptions = {},
) {
  return postJSON<ValidationIssueGroupRequest, ValidationIssueGroupResponse>(
    "/api/v1/validation/groups",
    request,
    options,
  );
}

export function createValidationRepairReview(
  request: ValidationRepairReviewCreateRequest,
  options: RequestOptions = {},
) {
  return postJSON<ValidationRepairReviewCreateRequest, ValidationRepairReview>(
    "/api/v1/validation/repair-reviews",
    request,
    options,
  );
}

export function getValidationRepairReview(id: string, options: RequestOptions = {}) {
  return fetchJSON<ValidationRepairReview>(
    `/api/v1/validation/repair-reviews/${encodeURIComponent(id)}`,
    requestInit(options),
  );
}

export function applyValidationRepairReview(
  id: string,
  request: ValidationRepairReviewApplyRequest,
  options: RequestOptions = {},
) {
  return postJSON<ValidationRepairReviewApplyRequest, ValidationRepairApplyResponse>(
    `/api/v1/validation/repair-reviews/${encodeURIComponent(id)}/apply`,
    request,
    options,
  );
}

export function queryPublicGraphQLOperation<TData, TVariables extends JsonObject = JsonObject>(
  query: string,
  variables?: TVariables,
  operationName?: string,
  options?: PublicReadOptions,
  requestOptions?: RequestOptions,
) {
  return postJSON<OntologyQueryReadRequest, PublicGraphQLResult<TData>>(
    "/api/v1/graphql",
    withEditSessionSnapshot(
      {
        query,
        variables,
        operationName,
      },
      options,
    ),
    requestOptions ?? options,
    true,
  );
}

export function getPublicNodeDetail(ref: string, options?: PublicReadOptions) {
  return queryPublicGraphQLOperation<PublicNodeDetailData>(
    PUBLIC_NODE_DETAIL_QUERY,
    { ref },
    "PublicNodeDetail",
    options,
  );
}

export function publicWorkspaceRef(ref: string | NodeRef): string {
  if (isString(ref)) return ref;

  if (ref.kind === "NOTE") return ref.notePath;

  if (ref.fragment?.startsWith("^")) {
    return `${ref.notePath}#${ref.fragment}`;
  }

  if (ref.structuralFingerprint) {
    return `${ref.notePath}#struct:${ref.structuralFingerprint}`;
  }

  if (ref.fragment) return `${ref.notePath}#${ref.fragment}`;

  if (ref.nodeId) return `${ref.notePath}#node:${ref.nodeId}`;

  return ref.notePath;
}

export function publicOntologyListItemRef(
  item: Pick<OntologyNoteListItem, "ref" | "path">,
): string {
  return publicWorkspaceRef(item.ref ?? item.path);
}

export async function getNodeWorkspace(
  ref: string | NodeRef,
  options?: PublicReadOptions,
): Promise<NodeWorkspace> {
  const requestedRef = publicWorkspaceRef(ref);
  const result = await getPublicNodeDetail(requestedRef, options);

  if (result.errors?.length && !result.data?.node) {
    throw new Error(result.errors[0]?.message || "Public GraphQL node failed");
  }

  if (!result.data) {
    throw new Error(`Public GraphQL node did not return data: ${requestedRef}`);
  }

  return nodeWorkspaceFromPublicGraphQL(result.data, requestedRef);
}

export function getPublicEventsUrl() {
  return "/api/v1/events";
}

export function listPublicViews(options: RequestOptions = {}) {
  return fetchJSON<ViewCatalog>("/api/v1/views", requestInit(options));
}

export function getPublicView(id: string) {
  return fetchJSON<ViewCatalogEntry>(`/api/v1/views/${encodeURIComponent(id)}`);
}

export function executePublicView(
  id: string,
  body: ViewExecuteRequest = {},
  options?: PublicReadOptions,
) {
  return postJSON<ViewExecuteReadRequest, ViewExecuteResponse>(
    `/api/v1/views/${encodeURIComponent(id)}/execute`,
    withEditSessionSnapshot(body, options),
    options,
  );
}

/** Writes view state to view YAML (SPEC-0112); 400 for an invalid result, 409 for a stale fingerprint. */
export function savePublicView(id: string, body: ViewSaveRequest) {
  return postJSON<ViewSaveRequest, ViewSaveResponse>(
    `/api/v1/views/${encodeURIComponent(id)}/save`,
    body,
  );
}

export function listPublicViewFieldCandidates(
  id: string,
  field: string,
  options: { q?: string; limit?: number } & PublicReadOptions = {},
) {
  const params = new URLSearchParams({ field });

  if (options.q) params.set("q", options.q);

  if (options.limit) params.set("limit", String(options.limit));

  return stagedRead<ViewFieldCandidatesResponse>(
    `/api/v1/views/${encodeURIComponent(id)}/field-candidates?${params.toString()}`,
    options,
  );
}

export function getTree(path?: string, limit: number = 200, requestOptions: RequestOptions = {}) {
  const suffix = path ? `?path=${encodeURIComponent(path)}&limit=${limit}` : "";

  return fetchJSON<TreeResponse>(`/api/v1/files/tree${suffix}`, requestInit(requestOptions));
}

export function getFileView(path: string, requestOptions: RequestOptions = {}) {
  return fetchJSON<FileView>(
    `/api/v1/files/view?path=${encodeURIComponent(path)}`,
    requestInit(requestOptions),
  );
}

export function getRenderedFile(path: string) {
  return fetchJSON<RenderedFile>(`/api/v1/files/rendered?path=${encodeURIComponent(path)}`);
}

export function getSuggestions(
  query: string,
  limit: number = 20,
  requestOptions: RequestOptions = {},
) {
  return fetchJSON<SuggestResponse>(
    `/api/v1/suggest?q=${encodeURIComponent(query)}&limit=${limit}`,
    requestInit(requestOptions),
  );
}

export function searchPaths(
  query: string,
  limit: number = 200,
  requestOptions: RequestOptions = {},
) {
  return fetchJSON<SearchResponse>(
    `/api/v1/search?q=${encodeURIComponent(query)}&limit=${limit}`,
    requestInit(requestOptions),
  );
}

export function searchWorkspace(
  query: string,
  options: {
    scope?: "all" | "notes" | "code";
    noteType?: string | null;
    folder?: string | null;
    limit?: number;
    continuationToken?: string | null;
  } = {},
  requestOptions: RequestOptions = {},
) {
  const params = new URLSearchParams({
    q: query,
    limit: String(options.limit ?? 40),
  });

  if (options.scope && options.scope !== "all") params.set("scope", options.scope);

  if (options.noteType) params.set("noteType", options.noteType);

  if (options.folder) params.set("folder", options.folder);

  if (options.continuationToken) params.set("continuationToken", options.continuationToken);

  return fetchJSON<WorkspaceSearchResponse>(
    `/api/v1/search?${params.toString()}`,
    requestInit(requestOptions),
  );
}

export function searchNotes(
  query: string,
  options: {
    type?: string;
    pathPrefix?: string;
    tag?: string;
    limit?: number;
    offset?: number;
  } = {},
  requestOptions: RequestOptions = {},
) {
  const params = new URLSearchParams({
    q: query,
    limit: String(options.limit ?? 25),
    offset: String(options.offset ?? 0),
  });

  if (options.type) params.set("type", options.type);

  if (options.pathPrefix) params.set("pathPrefix", options.pathPrefix);

  if (options.tag) params.set("tag", options.tag);

  return fetchJSON<NoteSearchResponse>(
    `/api/v1/search/notes?${params.toString()}`,
    requestInit(requestOptions),
  );
}

export function getOntologySummary(options: RequestOptions = {}) {
  return fetchJSON<OntologySummaryResponse>("/api/v1/ontology/summary", requestInit(options));
}

export function getOntologyType(name: string, options: PublicReadOptions = {}) {
  return stagedRead<OntologyTypeResponse>(
    `/api/v1/ontology/types/${encodeURIComponent(name)}`,
    options,
  );
}

export function getOntologyAtlas(options: RequestOptions = {}) {
  return fetchJSON<OntologyAtlasResponse>("/api/v1/ontology/atlas", requestInit(options));
}

export function getOntologyInspect(path: string) {
  return fetchJSON<OntologyInspectResponse>(
    `/api/v1/ontology/inspect?path=${encodeURIComponent(path)}`,
  );
}

export function getOntologyQuerySchema() {
  return fetchJSON<OntologyQuerySchemaResponse>("/api/v1/ontology/query-schema");
}

// The public GraphQL endpoint accepts queries only, so these POST reads may
// retry INDEX_INITIALIZING. Edit-session mutation POSTs keep the default policy.
export function queryOntology(body: OntologyQueryRequest) {
  return postJSON<OntologyQueryRequest, OntologyQueryResult>("/api/v1/graphql", body, {}, true);
}

function nodeWorkspaceParams(ref: string | NodeRef) {
  if (isString(ref)) {
    return new URLSearchParams({ ref });
  }

  const locator = ref.fragment ? `${ref.notePath}#${ref.fragment}` : ref.notePath;
  const params = new URLSearchParams({ ref: locator });

  if (ref.nodeId) params.set("nodeId", ref.nodeId);

  if (ref.structuralFingerprint) {
    params.set("structural", ref.structuralFingerprint);
  }

  if (ref.kind) params.set("kind", ref.kind);

  return params;
}

export function getNodePreview(ref: string, from?: string, options: PublicReadOptions = {}) {
  const params = new URLSearchParams({ ref });

  if (from) params.set("from", from);

  return stagedRead<NodePreview>(`/api/v1/nodes/preview?${params.toString()}`, options);
}

export async function getNodeWorkspaceGraph(
  ref: string | NodeRef,
  options?: PublicReadOptions,
): Promise<NodeWorkspaceGraph> {
  const requestedRef = publicWorkspaceRef(ref);

  const result = await queryPublicGraphQLOperation<PublicLocalGraphData>(
    PUBLIC_LOCAL_GRAPH_QUERY,
    { ref: requestedRef, nodeLimit: 500, edgeLimit: 1000 },
    "PublicLocalGraph",
    options,
  );

  if (result.errors?.length && !result.data?.node) {
    throw new Error(result.errors[0]?.message || "Public GraphQL graph failed");
  }

  if (!result.data?.node) {
    throw new Error(`Public GraphQL graph did not resolve: ${requestedRef}`);
  }

  const workspace = nodeWorkspaceFromPublicGraphQL({ node: result.data.node }, requestedRef);

  return {
    focusedNodeId: workspace.focusedNodeId,
    nodes: workspace.nodes,
    edges: workspace.edges,
    views: workspace.views,
  };
}

export function getNodeEventsUrl(refs: Array<string | NodeRef>) {
  const params = new URLSearchParams();
  refs.forEach((ref) => {
    const refParams = nodeWorkspaceParams(ref);
    const target = refParams.get("ref");

    if (target) {
      params.append("ref", target);
    }

    const nodeId = isString(ref) ? "" : ref.nodeId || "";
    const structural = isString(ref) ? "" : ref.structuralFingerprint || "";
    const kind = isString(ref) ? "" : ref.kind || "";
    params.append("nodeId", nodeId);
    params.append("structural", structural);
    params.append("kind", kind);
  });

  return `/api/v1/nodes/events?${params.toString()}`;
}

export function createOntologyEditSession(ops: OntologyEditOp[] = [], sessionId?: string) {
  return postJSON<{ sessionId?: string; ops?: OntologyEditOp[] }, OntologyEditSessionResponse>(
    "/api/v1/edit-sessions",
    { sessionId, ops },
  );
}

export function stageOntologyEditSession(
  sessionId: string,
  body: {
    requestId?: string;
    expectedRevision?: number;
    ops: OntologyEditOp[];
    replace?: boolean;
    snapshot?: OntologyEditSessionSnapshot;
  },
) {
  return postJSONAllowConflict<typeof body, OntologyEditSessionResponse>(
    `/api/v1/edit-sessions/${encodeURIComponent(sessionId)}/stage`,
    body,
  );
}

export function previewOntologyEditSession(
  sessionId: string,
  snapshot?: OntologyEditSessionSnapshot,
) {
  return postJSONAllowConflict<
    { snapshot?: OntologyEditSessionSnapshot },
    OntologyEditSessionResponse
  >(`/api/v1/edit-sessions/${encodeURIComponent(sessionId)}/preview`, {
    snapshot,
  });
}

export function commitOntologyEditSession(
  sessionId: string,
  snapshot?: OntologyEditSessionSnapshot,
  request?: { requestId?: string; expectedRevision?: number },
) {
  return postJSONAllowConflict<
    { snapshot?: OntologyEditSessionSnapshot; requestId?: string; expectedRevision?: number },
    OntologyEditSessionResponse
  >(`/api/v1/edit-sessions/${encodeURIComponent(sessionId)}/commit`, {
    snapshot,
    ...request,
  });
}

export function diffOntologyEditSession(sessionId: string, snapshot?: OntologyEditSessionSnapshot) {
  return postJSON<{ snapshot?: OntologyEditSessionSnapshot }, ModifiedNotesResponse>(
    `/api/v1/edit-sessions/${encodeURIComponent(sessionId)}/diff`,
    {
      snapshot,
    },
  );
}

export function deleteOntologyEditSession(sessionId: string) {
  return fetchJSON<{ deleted: boolean }>(`/api/v1/edit-sessions/${encodeURIComponent(sessionId)}`, {
    method: "DELETE",
  });
}

export function getAgentSettings(options: RequestOptions = {}) {
  return fetchJSON<AgentSettingsResponse>("/api/agent/settings", requestInit(options));
}

export function saveAgentSettings(settings: AgentSettings) {
  return fetchJSON<AgentSettingsResponse>("/api/agent/settings", {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ settings }),
  });
}

export function listAgentSessions(options: RequestOptions = {}) {
  return fetchJSON<AgentSessionsResponse>("/api/agent/sessions", requestInit(options));
}

export function createAgentSession(title?: string) {
  return postJSON<{ title?: string }, AgentSessionResponse>("/api/agent/sessions", { title });
}

export function getAgentSession(sessionId: string, options: RequestOptions = {}) {
  return fetchJSON<AgentSessionResponse>(
    `/api/agent/sessions/${encodeURIComponent(sessionId)}`,
    requestInit(options),
  );
}

export function deleteAgentSession(sessionId: string) {
  return fetchJSON<{ deleted: boolean }>(`/api/agent/sessions/${encodeURIComponent(sessionId)}`, {
    method: "DELETE",
  });
}

export function sendAgentMessage(sessionId: string, content: string) {
  return postJSON<{ content: string }, AgentSendMessageResponse>(
    `/api/agent/sessions/${encodeURIComponent(sessionId)}/messages`,
    { content },
  );
}

export function respondToAgentApproval(
  sessionId: string,
  requestId: string,
  decision: AgentApprovalDecision,
) {
  return postJSON<{ decision: AgentApprovalDecision }, { responded: boolean }>(
    `/api/agent/sessions/${encodeURIComponent(sessionId)}/approvals/${encodeURIComponent(requestId)}`,
    { decision },
  );
}

export function interruptAgentSession(sessionId: string) {
  return postJSON<Record<string, never>, { interrupted: boolean }>(
    `/api/agent/sessions/${encodeURIComponent(sessionId)}/interrupt`,
    {},
  );
}

export function getAgentEventsUrl(sessionId: string, after?: number) {
  const params = new URLSearchParams();

  if (after && after > 0) params.set("after", String(after));
  const suffix = params.size > 0 ? `?${params.toString()}` : "";

  return `/api/agent/sessions/${encodeURIComponent(sessionId)}/events${suffix}`;
}
