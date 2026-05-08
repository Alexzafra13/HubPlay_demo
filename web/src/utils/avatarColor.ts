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
// Palette: deep, saturated tones in the same brightness register so
// white initials read consistently. Mix of warm and cool — earthy
// (terracotta / bronze / olive), greens (moss / teal / cyan), blues
// (navy / slate / petrol), and warm purples (plum / wine / violet)
// — keeps the assignment feeling intentional, never neon.

export interface AvatarPalette {
  readonly background: string;
  readonly label: string;
}

export const AVATAR_PALETTE: readonly AvatarPalette[] = [
  { background: "#3d5a40", label: "moss" },       // verde musgo
  { background: "#7a3d2e", label: "terracotta" }, // terracota
  { background: "#1e3252", label: "navy" },       // azul marino
  { background: "#5c3d6e", label: "plum" },       // morado
  { background: "#2e5c5a", label: "teal" },       // verde azulado
  { background: "#7a5c2e", label: "bronze" },     // bronce
  { background: "#5a3d3d", label: "garnet" },     // granate apagado
  { background: "#3d4a5c", label: "slate" },      // pizarra
  { background: "#6e3d5c", label: "wine" },       // vino borgoña
  { background: "#3d6e6e", label: "cyan-deep" },  // cian apagado
  { background: "#5c4a2e", label: "coffee" },     // café oscuro
  { background: "#4a2e5c", label: "violet" },     // violeta profundo
  { background: "#2e4a5c", label: "petrol" },     // petróleo
  { background: "#5c5c2e", label: "olive" },      // olivo oscuro
];

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
