import type { Decorator } from "@storybook/react-vite";

import { HostFrame } from "./host-frames";
import { StoryProviders } from "./providers";

export const withHostFrame: Decorator = (Story, context) => {
  const theme = context.globals.theme === "dark" ? "dark" : "light";
  const frame: unknown = context.parameters.frame ?? context.globals.frame;
  const size = frame === "mobile" || frame === "narrow" ? frame : "leaf";
  return <StoryProviders><HostFrame size={size} theme={theme} largeText={context.parameters.fontScale === 2} shell={context.parameters.shell === true}><Story /></HostFrame></StoryProviders>;
};
