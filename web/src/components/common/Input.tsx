import type { InputHTMLAttributes, ReactNode, Ref } from "react";

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
  hint?: string;
  icon?: ReactNode;
  // React 19: `ref` viaja como prop normal. Declarado explícito para
  // los call sites que necesitan acceder al input (focus programático,
  // selección de texto al editar).
  ref?: Ref<HTMLInputElement>;
}

function Input({
  label,
  error,
  hint,
  icon,
  className = "",
  id,
  ref,
  ...props
}: InputProps) {
  const inputId = id ?? label?.toLowerCase().replace(/\s+/g, "-");

  return (
    <div className="flex flex-col gap-1.5">
      {label && (
        <label
          htmlFor={inputId}
          className="text-sm font-medium text-text-secondary"
        >
          {label}
        </label>
      )}

      <div className="relative">
        {icon && (
          <span className="absolute left-3 top-1/2 -translate-y-1/2 text-text-muted pointer-events-none">
            {icon}
          </span>
        )}

        <input
          ref={ref}
          id={inputId}
          className={[
            "w-full rounded-[--radius-md] bg-bg-card border px-3 py-2 text-sm",
            "text-text-primary placeholder:text-text-muted",
            "transition-colors duration-150",
            "focus:outline-none focus:border-accent focus:ring-1 focus:ring-accent/30",
            "disabled:opacity-50 disabled:cursor-not-allowed",
            error
              ? "border-error focus:border-error focus:ring-error/30"
              : "border-border",
            icon ? "pl-10" : "",
            className,
          ]
            .filter(Boolean)
            .join(" ")}
          {...props}
        />
      </div>

      {error && <p className="text-xs text-error">{error}</p>}
      {!error && hint && <p className="text-xs text-text-muted">{hint}</p>}
    </div>
  );
}

export { Input };
