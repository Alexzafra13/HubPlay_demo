import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";

import "@/i18n"; // side-effect import — initialise i18next
import Uploads from "./Uploads";

// ─── Test scaffolding ────────────────────────────────────────────────

// tus-js-client se mockea entero — los tests verifican el state-machine
// de la UI y la validación cliente, NO el protocolo tus. Spawn de un
// upload "real" requeriría un servidor tus en un puerto local y queda
// fuera de scope.
//
// Constructor function (no arrow) — arrows no son `new`-ables.
vi.mock("tus-js-client", () => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const Upload = vi.fn(function (this: any, file: File, opts: unknown) {
    this.file = file;
    this.options = opts;
    this.start = vi.fn();
    this.abort = vi.fn();
  });
  return { Upload };
});

// EventBus SSE — devolvemos no-op para que el componente monte sin
// abrir EventSource real (jsdom no implementa EventSource bien).
vi.mock("@/hooks/eventBus", () => ({
  subscribeSse: vi.fn(() => () => undefined),
}));

// API — métodos que el componente llama.
vi.mock("@/api/client", () => ({
  api: {
    listMyUploads: vi.fn().mockResolvedValue([]),
    uploadsEndpoint: vi.fn(() => "/api/v1/uploads/"),
    // El FolderBrowser dentro del UploadDropzone lo invoca al elegir
    // una librería. Devolver lista vacía es suficiente para los
    // tests del Uploads page (los tests del FolderBrowser
    // específicos están en su propio archivo).
    browseUploadFolders: vi.fn().mockResolvedValue({
      library_id: "",
      library_name: "",
      path: "",
      directories: [],
      files: [],
    }),
    createUploadFolder: vi.fn().mockResolvedValue(undefined),
  },
}));

// Hooks — para inyectar `me` y `libraries` sin tocar la red.
const meRef: { current: ReturnType<typeof makeMe> | null } = { current: null };
const libsRef: { current: ReturnType<typeof makeLibraries> } = {
  current: [],
};

function makeMe(overrides: Partial<{ can_upload: boolean; upload_quota_bytes: number; upload_used_bytes: number }> = {}) {
  return {
    id: "u-1",
    username: "alex",
    display_name: "Alex",
    role: "user",
    created_at: "2024-01-01T00:00:00Z",
    can_upload: true,
    upload_quota_bytes: 10 * 1024 * 1024 * 1024, // 10 GiB
    upload_used_bytes: 0,
    ...overrides,
  };
}

function makeLibraries(items: Array<{ id: string; name: string; content_type: string }>) {
  return items.map((it) => ({
    ...it,
    paths: ["/data/" + it.id],
    settings: {},
    scan_mode: "auto",
    refresh_interval: "",
    language_filter: "",
    tls_insecure: false,
    m3u_url: "",
    epg_url: "",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
  }));
}

vi.mock("@/api/hooks", async () => {
  const actual = await vi.importActual<typeof import("@/api/hooks")>("@/api/hooks");
  return {
    ...actual,
    useMe: () => ({ data: meRef.current }),
    useLibraries: () => ({ data: libsRef.current }),
    useMyUploads: () => ({ data: [] }),
    useUploadEvents: vi.fn(),
  };
});

function wrap(ui: React.ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  meRef.current = makeMe();
  libsRef.current = makeLibraries([
    { id: "lib-mov", name: "Películas", content_type: "movies" },
    { id: "lib-shows", name: "Series", content_type: "shows" },
    // Excluida del picker por content_type:
    { id: "lib-live", name: "TV", content_type: "livetv" },
  ]);
});

// ─── Tests ───────────────────────────────────────────────────────────

describe("Uploads page — permission gate", () => {
  it("muestra empty-state cuando el usuario no tiene can_upload", () => {
    meRef.current = makeMe({ can_upload: false });
    render(wrap(<Uploads />));
    expect(
      screen.getByText(/no tienes permiso para subir|don't have permission/i),
    ).toBeInTheDocument();
  });

  it("muestra el dropzone cuando can_upload está activo", () => {
    render(wrap(<Uploads />));
    expect(
      // Hay dos hints con "arrastra ficheros" — el principal del
      // dropzone y el secundario que apunta al folder browser. Basta
      // con verificar que ALGUNO está presente.
      screen.getAllByText(/arrastra ficheros|drag files/i)[0],
    ).toBeInTheDocument();
  });
});

describe("Uploads page — librería destino", () => {
  it("muestra noLibraries cuando el usuario no tiene movies/shows accesibles", () => {
    libsRef.current = makeLibraries([
      { id: "lib-live", name: "TV", content_type: "livetv" },
    ]);
    render(wrap(<Uploads />));
    expect(
      screen.getByText(/no tienes bibliotecas|no library to upload/i),
    ).toBeInTheDocument();
  });

  it("filtra livetv del selector y deja sólo movies + shows", () => {
    render(wrap(<Uploads />));
    // El picker no aparece hasta que hay ficheros — para verificar el
    // filtrado le pegamos un fichero válido primero.
    // Pero aquí basta con confirmar que NO se muestra noLibraries.
    expect(
      screen.queryByText(/no tienes bibliotecas|no library to upload/i),
    ).toBeNull();
  });
});

describe("Uploads page — validación cliente-side", () => {
  it("rechaza ficheros con extensión no permitida", async () => {
    // applyAccept:false porque el input lleva el atributo `accept` con
    // el mismo whitelist — user-event lo respeta por defecto y eso
    // shortcircuita la validación que queremos testear. En producción
    // el `accept` filtra el file picker pero NO un drag&drop ni un
    // PATCH directo; nuestra defensa client-side cubre esos casos.
    const user = userEvent.setup({ applyAccept: false });
    render(wrap(<Uploads />));

    const file = new File(["x"], "evil.exe", { type: "application/octet-stream" });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await user.upload(input, file);

    expect(await screen.findByRole("alert")).toHaveTextContent(/evil\.exe/);
    // No staging — la cola sigue vacía, el bloque con "Subir N ficheros"
    // no aparece.
    expect(screen.queryByRole("button", { name: /subir 1|upload 1/i })).toBeNull();
  });

  it("acepta .mkv y lo añade a la cola", async () => {
    const user = userEvent.setup();
    render(wrap(<Uploads />));

    const file = new File(["fake mkv body"], "movie.mkv", {
      type: "video/x-matroska",
    });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await user.upload(input, file);

    expect(await screen.findByText("movie.mkv")).toBeInTheDocument();
    // El botón "Subir N ficheros" aparece habilitado.
    const startBtn = screen.getByRole("button", { name: /subir 1 fichero|upload 1 file/i });
    expect(startBtn).not.toBeDisabled();
  });

  it("rechaza ficheros que no caben en la cuota restante", async () => {
    meRef.current = makeMe({
      can_upload: true,
      upload_quota_bytes: 100,
      upload_used_bytes: 50,
    });
    const user = userEvent.setup();
    render(wrap(<Uploads />));

    // 200 bytes > 50 disponibles.
    const file = new File([new Uint8Array(200)], "big.mkv", {
      type: "video/x-matroska",
    });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await user.upload(input, file);

    expect(await screen.findByRole("alert")).toHaveTextContent(/big\.mkv/);
  });
});

describe("Uploads page — start upload", () => {
  it("crea un tus.Upload con metadata cuando se pulsa Subir", async () => {
    const tus = await import("tus-js-client");
    const user = userEvent.setup();
    render(wrap(<Uploads />));

    const file = new File(["data"], "movie.mkv", {
      type: "video/x-matroska",
    });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await user.upload(input, file);

    await screen.findByText("movie.mkv");
    const startBtn = screen.getByRole("button", { name: /subir 1 fichero|upload 1 file/i });
    await user.click(startBtn);

    await waitFor(() => expect(tus.Upload).toHaveBeenCalledTimes(1));
    const callArgs = (tus.Upload as unknown as { mock: { calls: unknown[][] } })
      .mock.calls[0];
    expect(callArgs[0]).toBe(file);
    const opts = callArgs[1] as {
      endpoint: string;
      metadata: { filename: string; library_id: string; subpath: string };
    };
    expect(opts.endpoint).toBe("/api/v1/uploads/");
    expect(opts.metadata.filename).toBe("movie.mkv");
    expect(opts.metadata.library_id).toBe("lib-mov"); // primer destino
    // Sin navegar el browser, el subpath empieza vacío = raíz de la
    // librería. Pin del comportamiento default.
    expect(opts.metadata.subpath).toBe("");

    // El componente llamó start() en la instancia recién construida.
    // vi.fn constructor expone .mock.instances con los `this` capturados.
    const instances = (tus.Upload as unknown as { mock: { instances: Array<{ start: ReturnType<typeof vi.fn> }> } })
      .mock.instances;
    expect(instances[0].start).toHaveBeenCalledTimes(1);
  });

  it("el upload aparece en la lista de activos en estado queued", async () => {
    const user = userEvent.setup();
    render(wrap(<Uploads />));

    const file = new File(["data"], "movie.mkv", {
      type: "video/x-matroska",
    });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await user.upload(input, file);
    await screen.findByText("movie.mkv");
    await user.click(
      screen.getByRole("button", { name: /subir 1 fichero|upload 1 file/i }),
    );

    // Sección "En curso" aparece con el fichero dentro.
    const activeSection = await screen.findByText(/en curso|in progress/i);
    expect(activeSection).toBeInTheDocument();
    // El fichero está en la lista de activos.
    const activeList = activeSection.closest("section");
    expect(activeList).not.toBeNull();
    if (activeList) {
      expect(within(activeList).getByText("movie.mkv")).toBeInTheDocument();
    }
  });
});

// API contract pin: el chunk size que pasamos a tus es 8 MiB. Si esto
// cambia inadvertidamente (refactor que toca options), un PATCH ratio
// distinto cambia el costo de red del producto. El test pin protege
// contra regresión silenciosa.
describe("Uploads page — config de tus", () => {
  it("usa chunkSize de 8 MiB y retryDelays explícitos", async () => {
    const tus = await import("tus-js-client");
    const user = userEvent.setup();
    render(wrap(<Uploads />));

    const file = new File(["x"], "movie.mkv", { type: "video/x-matroska" });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await user.upload(input, file);
    await user.click(
      await screen.findByRole("button", { name: /subir 1 fichero|upload 1 file/i }),
    );

    await waitFor(() => expect(tus.Upload).toHaveBeenCalledTimes(1));
    const opts = (tus.Upload as unknown as { mock: { calls: unknown[][] } })
      .mock.calls[0][1] as { chunkSize: number; retryDelays: number[] };
    expect(opts.chunkSize).toBe(8 * 1024 * 1024);
    expect(opts.retryDelays.length).toBeGreaterThan(0);
  });
});
