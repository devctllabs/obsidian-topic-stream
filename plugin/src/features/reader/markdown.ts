/** Host rendering owns its child Component; disposal is available before async rendering settles. */
export interface MarkdownRendererPort {
  render: (markdown: string, target: HTMLElement) => { ready: Promise<void>; dispose: () => void };
}
import { createContext } from "react";


export const MarkdownRendererContext = createContext<MarkdownRendererPort | null>(null);
