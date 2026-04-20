import { categoryMeta } from "./categoryHelpers";

interface CategoryChipProps {
  label: string; // either a resolved category name or "all"
  icon?: string;
  count?: number;
  active: boolean;
  onClick: () => void;
}

/**
 * CategoryChip is the rounded pill used in the filter rail. It picks colour
 * and emoji from `categoryMeta` for known categories, and accepts explicit
 * icon/label overrides for synthetic chips like "All".
 */
export function CategoryChip({
  label,
  icon,
  count,
  active,
  onClick,
}: CategoryChipProps) {
  const meta = categoryMeta(label);
  const displayIcon = icon ?? meta.icon;

  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={[
        "group shrink-0 inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs font-medium transition-all whitespace-nowrap",
        active
          ? `${meta.accent} border-transparent ring-1 shadow-sm`
          : "border-white/10 bg-white/[0.03] text-text-secondary hover:bg-white/[0.07] hover:text-text-primary",
      ].join(" ")}
    >
      <span aria-hidden="true" className="text-sm leading-none">
        {displayIcon}
      </span>
      <span>{label}</span>
      {typeof count === "number" && (
        <span
          className={[
            "tabular-nums rounded-full px-1.5 py-px text-[10px] font-semibold",
            active ? "bg-white/20" : "bg-white/5 text-text-muted",
          ].join(" ")}
        >
          {count}
        </span>
      )}
    </button>
  );
}
