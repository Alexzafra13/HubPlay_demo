// UsersAdmin — focuses on the mobile card path (the new layout
// introduced in the Bloque C work). The desktop table is covered
// by a manual review, no test; the mobile path is the
// regression-prone one because it duplicates form controls and
// piles the actions into a kebab menu.
//
// Tests pin:
//   - When isMobile, no <table> is rendered (cards-only).
//   - When isMobile, the kebab opens the actions menu.
//   - Profile-row actions (`+ Perfil`) are hidden on profile rows.

import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import "@/i18n";

// Mock the hooks layer at the api boundary. The component reaches
// for ~10 hooks; we only care about getting initial data through
// and observing mutations fire.
const apiMock = vi.hoisted(() => ({
  getUsers: vi.fn(),
  getMe: vi.fn(),
  createUser: vi.fn(),
  deleteUser: vi.fn(),
  resetUserPassword: vi.fn(),
  setUserPIN: vi.fn(),
  setUserContentRating: vi.fn(),
  setUserRole: vi.fn(),
  setUserActive: vi.fn(),
  setUserAccess: vi.fn(),
  setUserDisplayName: vi.fn(),
  setUserAvatarColor: vi.fn(),
  createProfile: vi.fn(),
  switchProfile: vi.fn(),
  listProfiles: vi.fn(),
  getLibraries: vi.fn(),
  getUserLibraryAccess: vi.fn(),
  setUserLibraryAccess: vi.fn(),
  createPersonalIPTVLibrary: vi.fn(),
  // The personal-IPTV modal now embeds the same livetv subform the
  // /admin/libraries page uses, which calls /iptv/countries on
  // mount when the public tab is active. Mock it so the dropdown
  // renders deterministically instead of pegging on a real fetch.
  getPublicCountries: vi.fn(),
  preflightM3U: vi.fn(),
  // useRefreshM3U calls api.refreshM3U + opens a NoopEventSource via
  // the jsdom shim, so the mutation never resolves in tests. That's
  // fine — production fires-and-forgets too, the modal closes on
  // create.onSuccess without awaiting refresh. Tests just assert the
  // POST was attempted with the new library id.
  refreshM3U: vi.fn(),
}));
vi.mock("@/api/client", () => ({ api: apiMock }));

// Force the mobile breakpoint regardless of jsdom's matchMedia.
vi.mock("@/hooks/useIsMobile", () => ({
  useIsMobile: () => true,
}));

import UsersAdmin from "./UsersAdmin";
import { useAuthStore } from "@/store/auth";

const PARENT_ADMIN = {
  id: "u1",
  username: "alice",
  display_name: "Alice",
  role: "admin",
  is_active: true,
  has_pin: false,
  is_primary: true,
  created_at: "2026-05-01T00:00:00Z",
};
const PARENT_USER = {
  id: "u2",
  username: "bob",
  display_name: "Bob",
  role: "user",
  is_active: true,
  has_pin: false,
  created_at: "2026-05-02T00:00:00Z",
};
const KID_PROFILE = {
  id: "u3",
  username: "bob/kid",
  display_name: "Kid",
  role: "user",
  is_active: true,
  has_pin: true,
  parent_user_id: "u2",
  created_at: "2026-05-03T00:00:00Z",
};

function wrap(node: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter>{node}</MemoryRouter>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  for (const k of Object.keys(apiMock) as (keyof typeof apiMock)[]) {
    apiMock[k].mockReset();
  }
  apiMock.getUsers.mockResolvedValue([PARENT_ADMIN, PARENT_USER, KID_PROFILE]);
  apiMock.getMe.mockResolvedValue(PARENT_ADMIN);
  apiMock.listProfiles.mockResolvedValue([]);
  apiMock.getLibraries.mockResolvedValue([
    { id: "lib-movies", name: "Películas", content_type: "movies", item_count: 0 },
    { id: "lib-tv", name: "TV", content_type: "livetv", item_count: 0 },
  ]);
  apiMock.getUserLibraryAccess.mockResolvedValue({
    user_id: "u2",
    owner_id: "u2",
    library_ids: ["lib-movies"],
    is_inherited: false,
  });
  apiMock.setUserLibraryAccess.mockResolvedValue(undefined);
  apiMock.createPersonalIPTVLibrary.mockResolvedValue({
    id: "lib-new",
    name: "Lista de Bob",
    content_type: "livetv",
    m3u_url: "https://example.com/bob.m3u",
  });
  apiMock.getPublicCountries.mockResolvedValue([
    { code: "es", name: "España", flag: "🇪🇸" },
    { code: "fr", name: "Francia", flag: "🇫🇷" },
  ]);
  apiMock.preflightM3U.mockResolvedValue({
    status: "ok",
    message: "OK",
    elapsed_ms: 12,
  });
  apiMock.refreshM3U.mockResolvedValue({ channels_imported: 0 });
  apiMock.createUser.mockResolvedValue({
    id: "u-new",
    username: "newone",
    display_name: "New One",
    role: "user",
    password_change_required: false,
  });
  useAuthStore.setState({
    user: PARENT_ADMIN,
    isAuthenticated: true,
    bootstrapped: true,
  });
});

describe("UsersAdmin (mobile)", () => {
  it("renders cards instead of a table on mobile", async () => {
    render(wrap(<UsersAdmin />));
    await waitFor(() => expect(screen.getByText("alice")).toBeInTheDocument());
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("collapses profile children under their parent and expands on click", async () => {
    render(wrap(<UsersAdmin />));
    await waitFor(() => expect(screen.getByText("bob")).toBeInTheDocument());
    // Collapsed by default: kid not visible.
    expect(screen.queryByText("kid")).toBeNull();

    // The chevron button is on the parent's card. Bob has 1 child
    // so the expandable affordance exists; click it.
    fireEvent.click(
      screen.getByRole("button", { name: /mostrar miembros/i }),
    );
    expect(screen.getByText("kid")).toBeInTheDocument();
  });

  it("renders the kebab and opens an actions menu", async () => {
    render(wrap(<UsersAdmin />));
    await waitFor(() => expect(screen.getByText("bob")).toBeInTheDocument());

    // Each card has a kebab. Use the "bob" card (a regular user
    // — hits all the conditional actions).
    const bobCard = screen.getByText("bob").closest("li");
    expect(bobCard).not.toBeNull();
    const kebab = within(bobCard!).getByRole("button", {
      name: /acciones|actions/i,
    });
    fireEvent.click(kebab);

    // Menu open — + Perfil / Poner PIN / Reiniciar contraseña /
    // Eliminar visible. "Personalizar" se quitó: el usuario edita
    // su propio nombre y avatar desde su página de ajustes.
    expect(screen.getByRole("menu")).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: /personalizar/i })).toBeNull();
    expect(screen.getByRole("menuitem", { name: /\+ perfil/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /poner pin/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /reiniciar contraseña/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /eliminar|delete/i })).toBeInTheDocument();
  });

  it("hides + Perfil and Reiniciar contraseña on profile rows", async () => {
    render(wrap(<UsersAdmin />));
    await waitFor(() => expect(screen.getByText("bob")).toBeInTheDocument());

    // Expand bob's profile children
    fireEvent.click(
      screen.getByRole("button", { name: /mostrar miembros/i }),
    );

    const kidCard = screen.getByText("kid").closest("li");
    expect(kidCard).not.toBeNull();
    const kebab = within(kidCard!).getByRole("button", {
      name: /acciones|actions/i,
    });
    fireEvent.click(kebab);

    // Cambiar PIN + Eliminar still visible.
    expect(
      screen.getByRole("menuitem", { name: /cambiar pin/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("menuitem", { name: /eliminar|delete/i }),
    ).toBeInTheDocument();
    // But "+ Perfil" and "Reiniciar contraseña" are scoped out.
    expect(screen.queryByRole("menuitem", { name: /\+ perfil/i })).toBeNull();
    expect(
      screen.queryByRole("menuitem", { name: /reiniciar contraseña/i }),
    ).toBeNull();
  });

  // ─── Library access matrix ──────────────────────────────────────

  it("ships grant_library_ids in createUser when creating a top-level account", async () => {
    render(wrap(<UsersAdmin />));
    await waitFor(() => expect(screen.getByText("alice")).toBeInTheDocument());

    // Open Add User modal. The i18n fallback is "en" (and some keys
    // have English copy, others stay Spanish), so match either.
    fireEvent.click(
      screen.getByRole("button", { name: /agregar usuario|add user/i }),
    );
    // Wait for the libraries checkbox section to render — that's the
    // signal that the modal is fully mounted AND the libraries query
    // has resolved, so the pre-check seeding has fired.
    const moviesCheckbox = await screen.findByLabelText(/películas/i);
    const tvCheckbox = screen.getByLabelText(/^tv\b/i);
    await waitFor(() => expect(moviesCheckbox).toBeChecked());
    expect(tvCheckbox).toBeChecked();

    // Un-tick movies so we can prove the dirty set ships exactly the
    // intended ids — not "all" via a shortcut.
    fireEvent.click(moviesCheckbox);
    expect(moviesCheckbox).not.toBeChecked();
    expect(tvCheckbox).toBeChecked();

    // Fill the username and submit. Placeholder varies by locale
    // ("juanperez" in es, "johndoe" in en); accept either.
    const usernameInput = screen.getByPlaceholderText(/juanperez|johndoe/i);
    fireEvent.change(usernameInput, { target: { value: "newone" } });
    fireEvent.click(screen.getByRole("button", { name: /^(crear|create)$/i }));

    await waitFor(() => {
      expect(apiMock.createUser).toHaveBeenCalledTimes(1);
    });
    const payload = apiMock.createUser.mock.calls[0][0];
    expect(payload.username).toBe("newone");
    expect(payload.grant_library_ids).toEqual(["lib-tv"]);
  });

  it("loads and PUTs the matrix from the Bibliotecas kebab action", async () => {
    render(wrap(<UsersAdmin />));
    await waitFor(() => expect(screen.getByText("bob")).toBeInTheDocument());

    const bobCard = screen.getByText("bob").closest("li");
    fireEvent.click(
      within(bobCard!).getByRole("button", { name: /acciones|actions/i }),
    );
    fireEvent.click(
      screen.getByRole("menuitem", { name: /bibliotecas|libraries/i }),
    );

    // Modal opens and the GET fired against bob's id.
    await waitFor(() =>
      expect(apiMock.getUserLibraryAccess).toHaveBeenCalledWith("u2"),
    );

    // Server says: ["lib-movies"]. Ensure the matrix reflects it.
    const moviesCheckbox = await screen.findByLabelText(/películas/i);
    const tvCheckbox = screen.getByLabelText(/^tv\b/i);
    await waitFor(() => expect(moviesCheckbox).toBeChecked());
    expect(tvCheckbox).not.toBeChecked();

    // Tick TV → save → PUT carries the union.
    fireEvent.click(tvCheckbox);
    fireEvent.click(screen.getByRole("button", { name: /guardar|save/i }));

    await waitFor(() =>
      expect(apiMock.setUserLibraryAccess).toHaveBeenCalledWith("u2", [
        "lib-movies",
        "lib-tv",
      ]),
    );
  });

  it("routes the PUT against the parent owner_id for a profile target", async () => {
    // Override the GET to look like a profile target: bob/kid asks
    // for access, server normalises to parent and flags is_inherited.
    apiMock.getUserLibraryAccess.mockResolvedValue({
      user_id: "u3",
      owner_id: "u2",
      library_ids: [],
      is_inherited: true,
    });
    render(wrap(<UsersAdmin />));
    await waitFor(() => expect(screen.getByText("bob")).toBeInTheDocument());

    // Expand parent to reach kid.
    fireEvent.click(
      screen.getByRole("button", { name: /mostrar miembros/i }),
    );
    const kidCard = screen.getByText("kid").closest("li");
    fireEvent.click(
      within(kidCard!).getByRole("button", { name: /acciones|actions/i }),
    );
    fireEvent.click(
      screen.getByRole("menuitem", { name: /bibliotecas|libraries/i }),
    );

    // The inherited notice surfaces so the admin understands what
    // they're editing.
    await waitFor(() => {
      // i18n key uses <Trans> + <strong>; assert via the human text
      // fragment that always appears (the bold owner name).
      expect(
        screen.getByText(/perfiles bajo esa cuenta|profile under that account/i),
      ).toBeInTheDocument();
    });

    // Pick lib-tv, save. PUT must go to "u2", NOT "u3".
    fireEvent.click(screen.getByLabelText(/^tv\b/i));
    fireEvent.click(screen.getByRole("button", { name: /guardar|save/i }));

    await waitFor(() =>
      expect(apiMock.setUserLibraryAccess).toHaveBeenCalledWith("u2", ["lib-tv"]),
    );
  });

  // ─── Personal IPTV list shortcut ────────────────────────────────

  it("opens the personal IPTV modal and submits via the custom M3U URL path", async () => {
    render(wrap(<UsersAdmin />));
    await waitFor(() => expect(screen.getByText("bob")).toBeInTheDocument());

    const bobCard = screen.getByText("bob").closest("li");
    fireEvent.click(
      within(bobCard!).getByRole("button", { name: /acciones|actions/i }),
    );
    fireEvent.click(
      screen.getByRole("menuitem", { name: /lista iptv personal|personal iptv/i }),
    );

    // Modal opens; the name field is seeded with "Lista de Bob".
    const nameInput = await screen.findByLabelText(/^nombre$|^name$/i);
    expect((nameInput as HTMLInputElement).value).toMatch(/Bob/);

    // Switch to the "Personalizada" tab so the M3U URL input replaces
    // the public-iptv-org picker. The shared livetv subform defaults
    // to "Público" on open.
    fireEvent.click(
      screen.getByRole("tab", { name: /personalizada|custom/i }),
    );

    const m3uInput = await screen.findByLabelText(/url m3u|m3u url/i);
    fireEvent.change(m3uInput, {
      target: { value: "https://example.com/bob.m3u" },
    });

    fireEvent.click(
      screen.getByRole("button", { name: /crear lista|create list/i }),
    );

    await waitFor(() => {
      expect(apiMock.createPersonalIPTVLibrary).toHaveBeenCalledTimes(1);
    });
    const [userId, payload] = apiMock.createPersonalIPTVLibrary.mock.calls[0];
    expect(userId).toBe("u2");
    expect(payload.m3u_url).toBe("https://example.com/bob.m3u");
    expect(payload.name).toMatch(/Bob/);

    // The modal must also kick off the first M3U refresh — otherwise
    // the user lands on an empty channel list waiting for the next
    // scheduled job. Mirror the behaviour of /admin/libraries/new.
    await waitFor(() =>
      expect(apiMock.refreshM3U).toHaveBeenCalledWith("lib-new"),
    );
  });

  it("constructs the iptv-org URL when the public country tab is used", async () => {
    render(wrap(<UsersAdmin />));
    await waitFor(() => expect(screen.getByText("bob")).toBeInTheDocument());

    const bobCard = screen.getByText("bob").closest("li");
    fireEvent.click(
      within(bobCard!).getByRole("button", { name: /acciones|actions/i }),
    );
    fireEvent.click(
      screen.getByRole("menuitem", { name: /lista iptv personal|personal iptv/i }),
    );

    // Default tab is "Público" + "country". Wait for the public-
    // countries query to resolve so the dropdown has an "es" option
    // we can select — without this the change event silently no-ops
    // against the placeholder "Elige una opción…".
    await screen.findByRole("option", { name: /españa/i });
    const countrySelect = screen.getByLabelText(/país|country/i);
    fireEvent.change(countrySelect, { target: { value: "es" } });

    fireEvent.click(
      screen.getByRole("button", { name: /crear lista|create list/i }),
    );

    await waitFor(() => {
      expect(apiMock.createPersonalIPTVLibrary).toHaveBeenCalledTimes(1);
    });
    const [, payload] = apiMock.createPersonalIPTVLibrary.mock.calls[0];
    expect(payload.m3u_url).toBe(
      "https://iptv-org.github.io/iptv/countries/es.m3u",
    );
  });

  it("rejects a non-http(s) M3U URL inline before hitting the API", async () => {
    render(wrap(<UsersAdmin />));
    await waitFor(() => expect(screen.getByText("bob")).toBeInTheDocument());

    const bobCard = screen.getByText("bob").closest("li");
    fireEvent.click(
      within(bobCard!).getByRole("button", { name: /acciones|actions/i }),
    );
    fireEvent.click(
      screen.getByRole("menuitem", { name: /lista iptv personal|personal iptv/i }),
    );
    fireEvent.click(
      screen.getByRole("tab", { name: /personalizada|custom/i }),
    );

    const m3uInput = await screen.findByLabelText(/url m3u|m3u url/i);
    fireEvent.change(m3uInput, { target: { value: "file:///etc/passwd" } });
    fireEvent.click(
      screen.getByRole("button", { name: /crear lista|create list/i }),
    );

    // The inline validator catches it; no network call should fire.
    await waitFor(() =>
      expect(
        screen.getByText(/http:\/\/ o https:\/\/|http:\/\/ or https:\/\//i),
      ).toBeInTheDocument(),
    );
    expect(apiMock.createPersonalIPTVLibrary).not.toHaveBeenCalled();
  });

  it("hides 'Lista IPTV personal' on profile rows (grants only target the household owner)", async () => {
    render(wrap(<UsersAdmin />));
    await waitFor(() => expect(screen.getByText("bob")).toBeInTheDocument());

    fireEvent.click(
      screen.getByRole("button", { name: /mostrar miembros/i }),
    );
    const kidCard = screen.getByText("kid").closest("li");
    fireEvent.click(
      within(kidCard!).getByRole("button", { name: /acciones|actions/i }),
    );
    expect(
      screen.queryByRole("menuitem", { name: /lista iptv personal|personal iptv/i }),
    ).toBeNull();
  });
});
