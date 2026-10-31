import { Component, type ReactNode } from "react";

export class ViewErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  public state = { failed: false };
  public static getDerivedStateFromError(): { failed: boolean } { return { failed: true }; }
  public render() {
    if (!this.state.failed) return this.props.children;
    return <div className="topic-stream-root"><div className="topic-stream-message" role="alert">
      <h1>Could not display this view</h1><p>Your saved content is unchanged.</p>
      <button type="button" className="topic-stream-refresh" onClick={() => this.setState({ failed: false })}>Try again</button>
    </div></div>;
  }
}
