// BrandWordmark — full hubplay logotype (icon + text). Single source so a
// future logo refresh updates everywhere at once. Use this for Login,
// Setup wizard and the TopBar so the brand reads consistently across the
// unauthenticated and authenticated surfaces.

interface BrandWordmarkProps {
  height?: number;
  className?: string;
}

export function BrandWordmark({ height = 28, className }: BrandWordmarkProps) {
  return (
    <img
      src="/hubplay_icon.svg"
      alt="HubPlay"
      style={{ height, width: "auto" }}
      className={className}
      draggable={false}
    />
  );
}
