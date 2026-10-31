import createDOMPurify from "dompurify";

/** Static SVG geometry/text only: no scripting, CSS, remote references, HTML or animation. */
export function sanitizeSvg(bytes: ArrayBuffer): ArrayBuffer {
  const purify = createDOMPurify(window);
  purify.addHook("uponSanitizeAttribute", (_node, attribute) => {
    if (attribute.attrName !== "xmlns" && /url\s*\(|javascript:|data:|\/\//i.test(attribute.attrValue)) attribute.keepAttr = false;
  });
  const clean = purify.sanitize(new TextDecoder("utf-8", { fatal: true }).decode(bytes), {
    ALLOWED_TAGS: ["svg", "g", "path", "rect", "circle", "ellipse", "line", "polyline", "polygon", "text", "tspan", "title", "desc"],
    ALLOWED_ATTR: ["xmlns", "width", "height", "viewBox", "preserveAspectRatio", "x", "y", "dx", "dy", "cx", "cy", "r", "rx", "ry", "d", "points", "transform", "fill", "fill-opacity", "stroke", "stroke-width", "stroke-linecap", "stroke-linejoin", "stroke-miterlimit", "stroke-opacity", "opacity", "text-anchor", "font-size", "font-family"],
    ALLOW_DATA_ATTR: false, ALLOW_ARIA_ATTR: false, KEEP_CONTENT: false,
    NAMESPACE: "http://www.w3.org/2000/svg", PARSER_MEDIA_TYPE: "image/svg+xml"
  });
  const root = new DOMParser().parseFromString(clean, "image/svg+xml").documentElement;
  if (root.localName !== "svg" || root.namespaceURI !== "http://www.w3.org/2000/svg") throw new Error("This SVG could not be sanitized.");
  return new TextEncoder().encode(clean).buffer;
}
