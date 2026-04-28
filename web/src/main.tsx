import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "./App";
import "./styles/globals.css";
import "./i18n";

// Default staleTime: 60 seconds.
//
// Self-hosted media servers run on a tiny user count (one family, a
// handful of devices). The cost of refetching is negligible, so we
// optimise for freshness rather than for caching: a library scan that
// completes on device A should be visible on device B within the
// minute, not within five.
//
// Hooks whose data is genuinely static (epg-catalog, public-countries)
// or that benefit from a longer cache (available-images from third-
// party providers) override this with their own staleTime — search
// the api/hooks.ts file for `staleTime:` to see the deliberate ones.
const DEFAULT_STALE_TIME_MS = 60_000;

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: DEFAULT_STALE_TIME_MS,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
);
