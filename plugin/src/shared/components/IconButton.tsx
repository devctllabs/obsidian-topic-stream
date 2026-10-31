import type { ButtonHTMLAttributes } from "react";

/*!
 * refresh-cw: https://github.com/lucide-icons/lucide/blob/main/icons/refresh-cw.svg
 * ISC License — Copyright (c) 2026 Lucide Icons and Contributors
 * Permission to use, copy, modify, and/or distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */
const paths = {
  up: "m6 14 6-6 6 6",
  down: "m6 10 6 6 6-6",
  close: "m6 6 12 12M6 18 18 6",
  send: "m22 2-7 20-4-9-9-4 20-7ZM22 2 11 13",
  back: "m12 5-7 7 7 7M5 12h15",
  refresh: "M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8M21 3v5h-5M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16M8 16H3v5",
  tasks: "M9 6h11M9 12h11M9 18h11M3 6h.01M3 12h.01M3 18h.01",
  more: "M5 12h.01M12 12h.01M19 12h.01",
  settings: "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8M10 2h4l.6 3 2 .9 2.7-1 2 3.4-2.2 2v3.4l2.2 2-2 3.4-2.7-1-2 .9-.6 3h-4l-.6-3-2-.9-2.7 1-2-3.4 2.2-2v-3.4l-2.2-2 2-3.4 2.7 1 2-.9Z"
};

export function IconButton({ icon, label, className = "", ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { icon: keyof typeof paths; label: string }) {
  return <button type="button" className={`topic-stream-icon-button ${className}`} aria-label={label} title={label} {...props}>
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {icon === "more" ? [5, 12, 19].map((x) => <circle key={x} cx={x} cy="12" r="1" />) : <path d={paths[icon]} />}
    </svg>
  </button>;
}
