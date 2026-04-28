// Barrel re-export for the per-domain hooks under `./hooks/`. Existing
// call sites (`import { useFoo } from "@/api/hooks"`) keep working
// unchanged; new call sites can also reach the same hook via the
// narrower `@/api/hooks/<domain>` import if they prefer to avoid
// pulling the whole map.
//
// `queryKeys` is re-exported from its own module — every hook file
// imports it directly to avoid an inverse cycle through this barrel.

export { queryKeys } from "./queryKeys";

export * from "./hooks/auth";
export * from "./hooks/setup";
export * from "./hooks/users";
export * from "./hooks/media";
export * from "./hooks/progress";
export * from "./hooks/channels";
export * from "./hooks/iptv-admin";
export * from "./hooks/channel-health";
export * from "./hooks/providers";
export * from "./hooks/system";
export * from "./hooks/images";
export * from "./hooks/preferences";
