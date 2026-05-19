import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nextProvider } from "react-i18next";

import i18n from "@/i18n";
import { AdminPermissionsMatrix } from "./AdminPermissionsMatrix";
import { api } from "@/api/client";
import type { User } from "@/api/types";

// Mock api.setUserPermissions — los tests verifican que el componente
// la llama con los argumentos correctos, no la lógica del backend.
vi.mock("@/api/client", () => ({
  api: {
    setUserPermissions: vi.fn().mockResolvedValue({}),
  },
}));

function wrap(ui: React.ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return (
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>
    </I18nextProvider>
  );
}

function makeAdmin(overrides: Partial<User>): User {
  return {
    id: overrides.id || "u-test",
    username: overrides.username || "test",
    display_name: overrides.display_name || "Test",
    role: "admin",
    created_at: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

const OWNER = makeAdmin({
  id: "u-owner",
  username: "owner",
  display_name: "Owner",
  is_owner: true,
});

const ADMIN = makeAdmin({
  id: "u-admin",
  username: "alice",
  display_name: "Alice",
  is_owner: false,
  can_edit_metadata: true,
  can_change_artwork: false,
});

beforeEach(() => {
  vi.clearAllMocks();
});

describe("AdminPermissionsMatrix", () => {
  it("renders empty state when there are no admins", () => {
    render(wrap(<AdminPermissionsMatrix users={[]} me={OWNER} />));
    expect(
      screen.getByText(/no hay administradores secundarios|no secondary admins/i),
    ).toBeInTheDocument();
  });

  it("renders owner first with the 'Primary' badge", () => {
    render(
      wrap(
        <AdminPermissionsMatrix
          users={[ADMIN, OWNER]} // intentionally out of order
          me={OWNER}
        />,
      ),
    );
    const rows = screen.getAllByRole("row").slice(1); // drop thead
    // First data row = owner.
    expect(within(rows[0]).getByText("Owner")).toBeInTheDocument();
    expect(within(rows[0]).getAllByText(/principal|primary/i).length).toBeGreaterThan(0);
  });

  it("disables all checkboxes for the owner row", () => {
    render(wrap(<AdminPermissionsMatrix users={[OWNER]} me={OWNER} />));
    const ownerRow = screen.getAllByRole("row")[1];
    const boxes = within(ownerRow).getAllByRole("checkbox");
    expect(boxes.length).toBe(7);
    for (const box of boxes) {
      expect(box).toBeDisabled();
      expect(box).toBeChecked();
    }
  });

  it("owner can toggle a flag on a secondary admin", async () => {
    const user = userEvent.setup();
    render(wrap(<AdminPermissionsMatrix users={[OWNER, ADMIN]} me={OWNER} />));

    // Alice row, can_change_artwork column. Reuse the aria-label
    // we set in the component: "alice — Cambiar carátulas" / EN equivalent.
    const checkbox = screen.getByRole("checkbox", {
      name: /alice.*cambiar carátulas|alice.*change artwork/i,
    });
    expect(checkbox).not.toBeChecked();
    expect(checkbox).not.toBeDisabled();

    await user.click(checkbox);
    await waitFor(() => {
      expect(api.setUserPermissions).toHaveBeenCalledWith("u-admin", {
        can_change_artwork: true,
      });
    });
  });

  it("non-owner with can_manage_admins cannot toggle can_manage_admins on others", () => {
    const viewer = makeAdmin({
      id: "u-viewer",
      username: "viewer",
      is_owner: false,
      can_manage_admins: true,
    });
    render(
      wrap(
        <AdminPermissionsMatrix
          users={[viewer, ADMIN]}
          me={viewer}
        />,
      ),
    );

    const cm = screen.getByRole("checkbox", {
      name: /alice.*gestionar admins|alice.*manage admins/i,
    });
    expect(cm).toBeDisabled();
  });

  it("non-owner with can_manage_admins CAN toggle the other flags", () => {
    const viewer = makeAdmin({
      id: "u-viewer",
      username: "viewer",
      is_owner: false,
      can_manage_admins: true,
    });
    render(
      wrap(
        <AdminPermissionsMatrix
          users={[viewer, ADMIN]}
          me={viewer}
        />,
      ),
    );

    const editMeta = screen.getByRole("checkbox", {
      name: /alice.*editar metadatos|alice.*edit metadata/i,
    });
    expect(editMeta).not.toBeDisabled();
  });

  it("admin without can_manage_admins sees the matrix read-only", () => {
    const viewer = makeAdmin({
      id: "u-viewer",
      username: "viewer",
      is_owner: false,
      // Sin can_manage_admins ni is_owner.
    });
    render(
      wrap(
        <AdminPermissionsMatrix
          users={[viewer, ADMIN]}
          me={viewer}
        />,
      ),
    );

    // Cada checkbox de Alice debe estar disabled.
    const aliceBoxes = screen.getAllByRole("checkbox", {
      name: /^alice/i,
    });
    expect(aliceBoxes.length).toBe(7);
    for (const box of aliceBoxes) {
      expect(box).toBeDisabled();
    }
  });
});
