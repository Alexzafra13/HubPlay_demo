// avatarColor — deterministic mapping from a stable user identifier to
// a colour from the brand palette.
//
// Why deterministic instead of a DB-backed `users.avatar_color`:
// - the same user gets the same colour on every device with zero
//   sync, zero migration, zero endpoint plumbing
// - the username (or id) is stable, so the colour is stable
// - if we later let users override their colour from Settings, we
//   add a `users.avatar_color` column and let it shadow the hash
//   default — the deterministic path stays as the fallback
//
// Hash: a small FNV-1a 32-bit on the input string. Cheap, well-mixed,
// no crypto-grade properties needed (we only care that two different
// usernames rarely collide on the same colour). The palette is
// generous (14 entries) so a household of 4-6 users gets distinct
// colours and even shared deployments rarely repeat at a glance.
//
// Palette: 8 tonos saturados, ~45° apart en el círculo cromático, en la
// misma franja de luminosidad para que las iniciales blancas se lean
// igual en todas. Reducida desde 14 a 8 para que el picker muestre
// opciones claramente distintas en lugar de pares casi idénticos
// (moss/olive, terracotta/garnet, navy/slate/petrol). Los hex que
// quedaron fuera siguen funcionando en avatarColorForUser porque
// caen al helper FNV cuando no matchean — no hay backfill.

export interface AvatarPalette {
  readonly background: string;
  readonly label: string;
}

export const AVATAR_PALETTE: readonly AvatarPalette[] = [
  { background: "#b91c1c", label: "rojo" },
  { background: "#c2410c", label: "naranja" },
  { background: "#a16207", label: "ámbar" },
  { background: "#15803d", label: "verde" },
  { background: "#0f766e", label: "turquesa" },
  { background: "#1d4ed8", label: "azul" },
  { background: "#6d28d9", label: "violeta" },
  { background: "#be185d", label: "rosa" },
];

// avatarColorForUser is the higher-level helper most callers should
// reach for: prefers the user's `avatar_color` override (set via the
// per-profile customisation modal) and falls back to the
// deterministic FNV helper when the override is empty. Accepts a
// loose user-shaped object so call sites that already have a User /
// ProfileSummary don't have to massage the input.
export function avatarColorForUser(
  user:
    | { avatar_color?: string | null; username?: string | null }
    | null
    | undefined,
): AvatarPalette {
  const override = user?.avatar_color?.toLowerCase();
  if (override) {
    const match = AVATAR_PALETTE.find(
      (p) => p.background.toLowerCase() === override,
    );
    if (match) return match;
    // Unknown hex (legacy / hand-edited DB row). Fall through to the
    // deterministic helper so we never paint with something the
    // palette never agreed to. We don't render `override` directly:
    // a stale value would persist visually and the user couldn't
    // un-pick it from the UI list.
  }
  return avatarColorFor(user?.username ?? null);
}

export function avatarColorFor(seed: string | null | undefined): AvatarPalette {
  // Cold-start guard: when we don't yet know the user's identity (the
  // TopBar can briefly render before /me resolves), fall back to the
  // first palette entry so the avatar still has a solid background
  // instead of a blank circle that pops in.
  if (!seed) return AVATAR_PALETTE[0];
  // FNV-1a 32-bit. The constants are the standard FNV offset basis +
  // prime; charCodeAt over the full string keeps the spread on short
  // usernames where a simple sum-of-codes would cluster.
  let h = 0x811c9dc5;
  for (let i = 0; i < seed.length; i++) {
    h ^= seed.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  // >>> 0 forces unsigned for the modulo so we don't get a negative
  // index when the high bit is set on long strings.
  const idx = (h >>> 0) % AVATAR_PALETTE.length;
  return AVATAR_PALETTE[idx];
}
