import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";
import { MarkdownRendererContext } from "../../features/reader/markdown";
import { fixtureMarkdown } from "./markdown";

export function StoryProviders({ children }: { children: ReactNode }) {
  const [client] = useState(() => new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } }));
  useEffect(() => () => { client.clear(); }, [client]);
  return <QueryClientProvider client={client}><MarkdownRendererContext.Provider value={fixtureMarkdown}>{children}</MarkdownRendererContext.Provider></QueryClientProvider>;
}
