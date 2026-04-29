// Icon set for media surfaces (detail page kebab menu, hero badges,
// admin tools). Kept as plain inline SVG components — no sprite, no
// runtime icon library. Re-colour with `currentColor` so a parent
// `text-text-primary` (or hover variant) drives the stroke.
//
// All icons render at 4 w-4 by default to match the kebab-menu row
// height. Callers that need a different size pass `className` to
// override; the path stays untouched.
//
// Each icon is a 24×24 viewBox with strokeWidth=2 — the size used by
// the existing kebab menu rows and the hero meta badges. Adding a
// new icon here is preferable to re-pasting the SVG inline at the
// call site so the path lives in exactly one place.

import type { SVGProps } from "react";

type IconProps = SVGProps<SVGSVGElement>;

const baseProps = {
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 2,
} as const;

export function ImageIcon({ className = "h-4 w-4", ...rest }: IconProps) {
  return (
    <svg {...baseProps} className={className} {...rest}>
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M2.25 15.75l5.159-5.159a2.25 2.25 0 013.182 0l5.159 5.159m-1.5-1.5l1.409-1.409a2.25 2.25 0 013.182 0l2.909 2.909M3.75 21h16.5A2.25 2.25 0 0022.5 18.75V5.25A2.25 2.25 0 0020.25 3H3.75A2.25 2.25 0 001.5 5.25v13.5A2.25 2.25 0 003.75 21z"
      />
    </svg>
  );
}

export function RefreshIcon({ className = "h-4 w-4", ...rest }: IconProps) {
  return (
    <svg {...baseProps} className={className} {...rest}>
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182"
      />
    </svg>
  );
}

export function InfoIcon({ className = "h-4 w-4", ...rest }: IconProps) {
  return (
    <svg {...baseProps} className={className} {...rest}>
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.853l.041-.021M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3.75h.008v.008H12V8.25z"
      />
    </svg>
  );
}

export function ExternalLinkIcon({ className = "h-4 w-4", ...rest }: IconProps) {
  return (
    <svg {...baseProps} className={className} {...rest}>
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        d="M13.5 6H5.25A2.25 2.25 0 003 8.25v10.5A2.25 2.25 0 005.25 21h10.5A2.25 2.25 0 0018 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25"
      />
    </svg>
  );
}
