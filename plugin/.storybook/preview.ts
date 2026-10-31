import type { Preview } from "@storybook/react-vite";

import "./obsidian-theme.css";
import "../styles.css";
import "../src/test/storybook/host-frame.css";
import { withHostFrame } from "../src/test/storybook/decorators";

const preview: Preview = {
  decorators: [withHostFrame],
  globalTypes: {
    theme: { description: "Obsidian theme", toolbar: { icon: "paintbrush", items: ["light", "dark"], dynamicTitle: true } },
    frame: { description: "Workspace leaf width", toolbar: { icon: "browser", items: ["leaf", "narrow", "mobile"], dynamicTitle: true } }
  },
  initialGlobals: { theme: "dark", frame: "leaf" },
  parameters: { layout: "fullscreen", a11y: { test: "error" }, controls: { expanded: true } }
};

export default preview;
