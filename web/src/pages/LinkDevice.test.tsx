import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

vi.mock("@/api/client", () => ({
  api: {
    approveDeviceCode: vi.fn(),
    listMySessions: vi.fn().mockResolvedValue([]),
  },
}));

// Importing the i18n module wires LinkDevice's useTranslation() to
// real es/en resources. Without this, t("link.approve") would render
// the raw key and the role-name regex below would never match.
import "@/i18n";
import { api } from "@/api/client";
import LinkDevice from "./LinkDevice";

const apiMock = api as unknown as {
  approveDeviceCode: ReturnType<typeof vi.fn>;
  listMySessions: ReturnType<typeof vi.fn>;
};

function wrap(initialEntry: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <MemoryRouter initialEntries={[initialEntry]}>
      <QueryClientProvider client={client}>
        <LinkDevice />
      </QueryClientProvider>
    </MemoryRouter>
  );
}

beforeEach(() => {
  apiMock.approveDeviceCode.mockReset();
  apiMock.listMySessions.mockReset();
  apiMock.listMySessions.mockResolvedValue([]);
});

describe("LinkDevice", () => {
  // The QR on the TV encodes verification_uri_complete which lands
  // here with the user_code already in the query string. The form
  // must pre-fill so the operator only taps Aprobar — no typing.
  it("prefills the code input from the ?code= URL parameter", async () => {
    render(wrap("/link?code=ABCD-EFGH"));
    const input = (await screen.findByPlaceholderText(
      /ABCD-EFGH/i,
    )) as HTMLInputElement;
    expect(input.value).toBe("ABCD-EFGH");
  });

  // Approve sends the canonicalised form (no dashes, uppercase) so
  // the backend's canonicalUserCode never has to guess the input
  // format. Even when the URL provides the dashed display variant.
  it("submits the canonicalised user code to the API", async () => {
    apiMock.approveDeviceCode.mockResolvedValueOnce({ approved: true });
    render(wrap("/link?code=abcd-efgh"));

    // Match either language — the i18n module's fallbackLng is "en"
    // so jsdom usually resolves to Approve device, but a contributor
    // running with es locale should not have to update the test.
    fireEvent.click(
      await screen.findByRole("button", { name: /aprobar|approve/i }),
    );

    await vi.waitFor(() =>
      expect(apiMock.approveDeviceCode).toHaveBeenCalledWith("ABCDEFGH"),
    );
  });
});
