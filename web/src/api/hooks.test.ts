// Barrel re-export contract test.
//
// Every existing call site in the SPA imports its hook from
// `@/api/hooks` (~50 sites at last count). The barrel is a single
// `export * from "./hooks/<domain>"` line per sub-file; if a future
// refactor renames a hook in a sub-file but forgets to keep the
// re-export wiring intact, every consumer breaks at runtime.
//
// This test imports a representative sample of public API from the
// barrel and asserts they're functions. It's intentionally
// shallow — we don't call the hooks (that requires a React render
// tree) — we just confirm the barrel resolves them. If anyone ever
// drops a sub-module from `hooks.ts` by accident, the names below
// flip to `undefined` and this test fails loudly.

import { describe, it, expect } from "vitest";
import * as hooks from "./hooks";

describe("api/hooks barrel", () => {
  it("re-exports queryKeys", () => {
    expect(hooks.queryKeys).toBeDefined();
    expect(typeof hooks.queryKeys).toBe("object");
    expect(Array.isArray(hooks.queryKeys.me)).toBe(true);
  });

  it("re-exports auth hooks", () => {
    expect(hooks.useMe).toBeTypeOf("function");
    expect(hooks.useLogin).toBeTypeOf("function");
    expect(hooks.useLogout).toBeTypeOf("function");
  });

  it("re-exports media hooks", () => {
    expect(hooks.useLibraries).toBeTypeOf("function");
    expect(hooks.useItems).toBeTypeOf("function");
    expect(hooks.useItem).toBeTypeOf("function");
    expect(hooks.useCreateLibrary).toBeTypeOf("function");
    expect(hooks.useScanLibrary).toBeTypeOf("function");
  });

  it("re-exports channels hooks", () => {
    expect(hooks.useChannels).toBeTypeOf("function");
    expect(hooks.useAddChannelFavorite).toBeTypeOf("function");
    expect(hooks.useRemoveChannelFavorite).toBeTypeOf("function");
    expect(hooks.useBulkSchedule).toBeTypeOf("function");
  });

  it("re-exports the three iptv-admin sub-domains", () => {
    // iptv-admin.ts: refresh + import + countries
    expect(hooks.useRefreshM3U).toBeTypeOf("function");
    expect(hooks.useRefreshEPG).toBeTypeOf("function");
    expect(hooks.useImportPublicIPTV).toBeTypeOf("function");
    expect(hooks.usePublicCountries).toBeTypeOf("function");
    // iptv-sources.ts: EPG source CRUD + catalogue
    expect(hooks.useEPGCatalog).toBeTypeOf("function");
    expect(hooks.useLibraryEPGSources).toBeTypeOf("function");
    expect(hooks.useAddEPGSource).toBeTypeOf("function");
    expect(hooks.useRemoveEPGSource).toBeTypeOf("function");
    expect(hooks.useReorderEPGSources).toBeTypeOf("function");
    // iptv-jobs.ts: scheduled jobs
    expect(hooks.useScheduledJobs).toBeTypeOf("function");
    expect(hooks.useUpsertScheduledJob).toBeTypeOf("function");
    expect(hooks.useDeleteScheduledJob).toBeTypeOf("function");
    expect(hooks.useRunScheduledJobNow).toBeTypeOf("function");
  });

  it("re-exports the rest of the per-domain hooks", () => {
    // channel-health
    expect(hooks.useUnhealthyChannels).toBeTypeOf("function");
    expect(hooks.useResetChannelHealth).toBeTypeOf("function");
    expect(hooks.useChannelsWithoutEPG).toBeTypeOf("function");
    // progress
    expect(hooks.useUpdateProgress).toBeTypeOf("function");
    expect(hooks.useToggleFavorite).toBeTypeOf("function");
    // images
    expect(hooks.useItemImages).toBeTypeOf("function");
    expect(hooks.useUploadImage).toBeTypeOf("function");
    // preferences
    expect(hooks.useMyPreferences).toBeTypeOf("function");
    expect(hooks.useUserPreference).toBeTypeOf("function");
    // setup
    expect(hooks.useSetupStatus).toBeTypeOf("function");
    expect(hooks.useSetupCreateAdmin).toBeTypeOf("function");
    // users / providers / system
    expect(hooks.useUsers).toBeTypeOf("function");
    expect(hooks.useProviders).toBeTypeOf("function");
    expect(hooks.useHealth).toBeTypeOf("function");
    expect(hooks.useSystemStats).toBeTypeOf("function");
    expect(hooks.useAuthKeys).toBeTypeOf("function");
  });
});
