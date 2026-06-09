// Arnés SOLO de verificación (no entra en el build de producción: index.html
// es el único input de `vite build`). Renderiza MediaGrid con N ítems
// sintéticos dentro de los mismos providers que la app real, para que un
// script de Playwright mida cuántas tarjetas hay realmente en el DOM y
// compruebe que la virtualización recicla en vez de acumular.
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "@/styles/globals.css";
import "@/i18n";
import { MediaGrid } from "@/components/media";
import type { MediaItem } from "@/api/types";

const COUNT = Number(new URLSearchParams(location.search).get("count") ?? "5000");
const COLORS = ["#1f2937", "#7f1d1d", "#064e3b", "#1e3a8a", "#581c87", "#7c2d12"];

const items = Array.from({ length: COUNT }, (_, i) => ({
  id: `item-${i}`,
  type: "movie",
  title: `Movie ${i}`,
  poster_url: null,
  poster_color: COLORS[i % COLORS.length],
  community_rating: null,
  year: 2000 + (i % 25),
})) as unknown as MediaItem[];

const qc = new QueryClient();
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <div style={{ padding: 16 }}>
          <MediaGrid items={items} loading={false} />
        </div>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
);
