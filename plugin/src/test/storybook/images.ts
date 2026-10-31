import type { ItemImageDownload } from "../../api/contracts";

/** Authored geometric test image, supplied only through the external image-download port. */
export function fixtureImage(): ItemImageDownload {
  const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="480" height="200" viewBox="0 0 480 200"><rect width="480" height="200" rx="12" fill="#2b2a29"/><g fill="#f2b38a"><rect x="32" y="72" width="100" height="56" rx="8"/><rect x="190" y="72" width="100" height="56" rx="8"/><rect x="348" y="72" width="100" height="56" rx="8"/></g><g stroke="#f3f0ed" stroke-width="2"><path d="M142 100h38m-8-7 8 7-8 7M300 100h38m-8-7 8 7-8 7"/></g><g fill="#241a13" font-family="sans-serif" font-size="14" text-anchor="middle"><text x="82" y="105">Source</text><text x="240" y="105">Index</text><text x="398" y="105">Results</text></g></svg>';
  return { bytes: new TextEncoder().encode(svg).buffer, contentType: "image/svg+xml" };
}
