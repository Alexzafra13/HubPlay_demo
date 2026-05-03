// BrandMark — the play-icon-in-rounded-square monogram. Used by both
// TopBar (as the wordmark anchor) and any future placement (login,
// setup wizard, error pages). Single source so a future logo refresh
// updates everywhere at once.

interface BrandMarkProps {
  size?: number;
}

export function BrandMark({ size = 32 }: BrandMarkProps) {
  const inner = Math.round(size * 0.5);
  return (
    <span
      className="relative inline-flex items-center justify-center rounded-lg bg-accent/10 ring-1 ring-accent/20 flex-shrink-0"
      style={{ width: size, height: size }}
    >
      <span
        className="absolute inset-0 rounded-lg opacity-60 blur-md"
        style={{
          background:
            "radial-gradient(circle at 30% 30%, var(--color-accent-glow), transparent 65%)",
        }}
        aria-hidden
      />
      <svg
        viewBox="0 0 24 24"
        className="relative text-accent fill-current"
        style={{ width: inner, height: inner }}
        aria-hidden
      >
        <path d="M8 5.5v13l11-6.5L8 5.5z" />
      </svg>
    </span>
  );
}
