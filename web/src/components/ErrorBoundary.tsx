import { Component, type ErrorInfo, type ReactNode } from "react";

type Props = {
  children: ReactNode;
  label?: string;
  resetKey?: unknown;
};

type State = {
  error: Error | null;
  info: ErrorInfo | null;
};

export class ErrorBoundary extends Component<Props, State> {
  state: State = {
    error: null,
    info: null,
  };

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    this.setState({ info });
    console.error("Rhizome render error", error, info);
  }

  componentDidUpdate(prevProps: Props) {
    if (prevProps.resetKey !== this.props.resetKey && this.state.error) {
      this.reset();
    }
  }

  reset = () => {
    this.setState({ error: null, info: null });
  };

  render() {
    if (!this.state.error) return this.props.children;

    const details = [
      this.state.error.stack || this.state.error.message,
      this.state.info?.componentStack,
    ]
      .filter(Boolean)
      .join("\n\n");

    return (
      <main className="app-error-boundary" role="alert">
        <p className="app-error-boundary__eyebrow">View error</p>
        <h1>{this.props.label || "Rhizome hit a render error"}</h1>
        <p>{this.state.error.message || "The current view could not render."}</p>
        <div className="app-error-boundary__actions">
          <button type="button" onClick={this.reset}>
            Try again
          </button>
          <button type="button" onClick={() => window.location.reload()}>
            Reload
          </button>
        </div>
        {details ? (
          <details className="app-error-boundary__details">
            <summary>Error details</summary>
            <pre>{details}</pre>
          </details>
        ) : null}
      </main>
    );
  }
}
