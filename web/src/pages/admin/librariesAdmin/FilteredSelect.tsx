// FilteredSelect — a native <select> whose option list is narrowed by a
// text filter provided from outside.
//
// Native <select> rather than a custom combobox because:
//   - keyboard-accessible out of the box,
//   - mobile picker integration is automatic,
//   - matches the rest of the admin's visual language.
//
// The filter input lives in the parent so every kind (country /
// category / language / region) can share one search box and the user
// doesn't have to clear it when switching tabs.

interface FilteredSelectProps {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  filter: string;
  loading?: boolean;
  options: { code: string; name: string }[];
}

export function FilteredSelect({
  id,
  label,
  value,
  onChange,
  filter,
  loading,
  options,
}: FilteredSelectProps) {
  const q = filter.trim().toLowerCase();
  const filtered = q
    ? options.filter(
        (o) =>
          o.name.toLowerCase().includes(q) ||
          o.code.toLowerCase().includes(q),
      )
    : options;

  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="text-sm font-medium text-text-secondary">
        {label}
        {q && (
          <span className="ml-2 text-[10px] font-normal text-text-muted">
            {filtered.length}/{options.length}
          </span>
        )}
      </label>
      <select
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        required
        className="w-full rounded-[--radius-md] bg-bg-card border border-border px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/30"
      >
        <option value="" disabled>
          {loading ? "Cargando…" : "Elige una opción…"}
        </option>
        {filtered.map((o) => (
          <option key={o.code} value={o.code}>
            {o.name} ({o.code})
          </option>
        ))}
      </select>
    </div>
  );
}
