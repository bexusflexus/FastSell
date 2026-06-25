import { Component, type ErrorInfo, type ReactNode } from 'react';

interface AppErrorBoundaryProps {
  children: ReactNode;
}

interface AppErrorBoundaryState {
  error: Error | null;
}

export class AppErrorBoundary extends Component<AppErrorBoundaryProps, AppErrorBoundaryState> {
  state: AppErrorBoundaryState = {
    error: null,
  };

  static getDerivedStateFromError(error: Error): AppErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('FastSell frontend render failed', error, errorInfo);
  }

  render() {
    if (this.state.error) {
      return (
        <div className="mx-auto max-w-3xl px-4 py-10 text-stone-100">
          <section className="rounded-lg border border-red-400/35 bg-graphite-900 p-5 shadow-panel">
            <p className="text-xs font-semibold uppercase tracking-[0.22em] text-red-200">Render failure</p>
            <h1 className="mt-2 text-xl font-semibold text-stone-50">FastSell could not render this page.</h1>
            <pre className="mt-4 overflow-auto rounded-md border border-red-400/20 bg-graphite-950 p-4 text-sm text-red-100">
              {this.state.error.message}
            </pre>
          </section>
        </div>
      );
    }

    return this.props.children;
  }
}
