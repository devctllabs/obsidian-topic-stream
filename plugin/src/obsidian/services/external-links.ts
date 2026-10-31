export function externalUrl(value: string): string | undefined {
  try {
    const url = new URL(value);
    return url.protocol === "https:" || url.protocol === "http:" ? url.toString() : undefined;
  } catch { return undefined; }
}

export function openExternalUrl(browser: Pick<Window, "open"> | null, value: string): void {
  const url = externalUrl(value);
  if (!url) throw new Error("This Item does not have a supported web URL.");
  if (!browser) throw new Error("The browser is unavailable. Try opening the original again.");
  browser.open(url, "_blank", "noopener,noreferrer");
}
