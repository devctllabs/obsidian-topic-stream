import type { StorybookConfig } from "@storybook/react-vite";

const config: StorybookConfig = {
  framework: "@storybook/react-vite",
  stories: ["../src/**/*.stories.@(ts|tsx)"],
  addons: ["@storybook/addon-docs", "@storybook/addon-a11y", "@storybook/addon-vitest"],
  core: { disableTelemetry: true },
  viteFinal: (vite) => ({ ...vite, optimizeDeps: {
    ...vite.optimizeDeps,
    include: [...(vite.optimizeDeps?.include ?? []), "react-dom", "react-hook-form", "@hookform/resolvers/zod", "zod", "dompurify"]
  } })
};

export default config;
