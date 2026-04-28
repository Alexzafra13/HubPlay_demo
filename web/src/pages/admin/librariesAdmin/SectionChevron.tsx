// Inline chevron — points right when collapsed, rotates 90° when open.
// 14 px is the visual weight that pairs with the 11–13 px section
// labels in `LibrariesAdmin`.

export function SectionChevron({ open }: { open: boolean }) {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={[
        "shrink-0 transition-transform duration-150",
        open ? "rotate-90" : "",
      ].join(" ")}
      aria-hidden
    >
      <polyline points="7 4 13 10 7 16" />
    </svg>
  );
}
