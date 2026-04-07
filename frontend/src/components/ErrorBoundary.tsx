"use client";
import { Component, ReactNode } from "react";

interface Props {
  children: ReactNode;
  fallback?: ReactNode;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error };
  }

  render() {
    if (this.state.hasError) {
      return (
        this.props.fallback || (
          <div className="flex flex-col items-center justify-center min-h-[50vh] text-center p-6">
            <div className="text-5xl mb-4">&#x26A0;&#xFE0F;</div>
            <h2 className="text-lg font-bold text-white mb-2">
              Something went wrong
            </h2>
            <p className="text-sm text-gray-500 mb-4">
              {this.state.error?.message}
            </p>
            <button
              onClick={() => this.setState({ hasError: false, error: null })}
              className="bg-lotus text-white px-4 py-2 rounded-lg text-sm"
            >
              Try Again
            </button>
          </div>
        )
      );
    }
    return this.props.children;
  }
}
