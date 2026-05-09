// UsersAdmin — focuses on the mobile card path (the new layout
// introduced in the Bloque C work). The desktop table is covered
// by a manual review, no test; the mobile path is the
// regression-prone one because it duplicates form controls and
// piles the actions into a kebab menu.
//
// Tests pin:
//   - When isMobile, no <table> is rendered (cards-only).
//   - When isMobile, the kebab opens the actions menu.
//   - The kebab "Personalizar" item opens the rename modal.
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
}));
vi.mock("@/api/client", () => ({ api: apiMock }));

// Force the mobile breakpoint regardless of jsdom's matchMedia.
vi.mock("@/hooks/useIsMobile", () => ({
  useIsMobile: () => true,
}));

// FederationAdmin is a heavy descendant unrelated to the path we
// care about. Stub it so the test stays focused.
vi.mock("./FederationAdmin", () => ({
  default: () => <div data-testid="federation-stub" />,
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

    // Now the menu is open — Personalizar / + Perfil / Poner PIN /
    // Reiniciar contraseña / Eliminar visible.
    expect(screen.getByRole("menu")).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /personalizar/i })).toBeInTheDocument();
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

    // Personalizar + Cambiar PIN + Eliminar still visible.
    expect(
      screen.getByRole("menuitem", { name: /personalizar/i }),
    ).toBeInTheDocument();
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

  it("opens the Personalizar modal from the kebab", async () => {
    render(wrap(<UsersAdmin />));
    await waitFor(() => expect(screen.getByText("bob")).toBeInTheDocument());

    const bobCard = screen.getByText("bob").closest("li");
    fireEvent.click(
      within(bobCard!).getByRole("button", { name: /acciones|actions/i }),
    );
    fireEvent.click(
      screen.getByRole("menuitem", { name: /personalizar/i }),
    );

    // Modal title surfaces.
    await waitFor(() =>
      expect(screen.getByText(/personalizar perfil/i)).toBeInTheDocument(),
    );
  });
});
