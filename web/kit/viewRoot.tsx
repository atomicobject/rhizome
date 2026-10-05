import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { Component, type ReactNode } from "react";

export function ViewError({ title, detail }: { title: string; detail: string }) {
  return (
    <div
      role="alert"
      style={{ padding: 16, font: "13px/1.5 ui-monospace, monospace", color: "#9c1d24" }}
    >
      <strong>{title}</strong>
      <pre style={{ whiteSpace: "pre-wrap", margin: "8px 0 0" }}>{detail}</pre>
    </div>
  );
}

type BoundaryState = { error: Error | null };

class ViewErrorBoundary extends Component<{ children: ReactNode }, BoundaryState> {
  state: BoundaryState = { error: null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  render() {
    if (this.state.error)
      return (
        <ViewError title="This view threw while rendering" detail={this.state.error.message} />
      );

    return this.props.children;
  }
}

/** The providers every mounted view renders inside, shared by mountView and the test harness. */
export function ViewRoot({
  queryClient,
  children,
}: {
  queryClient: QueryClient;
  children: ReactNode;
}) {
  return (
    <QueryClientProvider client={queryClient}>
      <ViewErrorBoundary>{children}</ViewErrorBoundary>
    </QueryClientProvider>
  );
}
