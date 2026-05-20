import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { CopyToClipboardButton } from "./CopyToClipboardButton";

// userEvent.setup() instala su propio polyfill de navigator.clipboard
// en jsdom — espiamos su writeText con vi.spyOn y restauramos en
// afterEach. Es más sólido que defineProperty porque no peleamos con
// el polyfill del runtime de tests.

describe("CopyToClipboardButton", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renderiza con aria-label accesible", () => {
    render(
      <CopyToClipboardButton value="hubplay.local:8096" label="Copiar URL" />,
    );
    expect(
      screen.getByRole("button", { name: /copiar url/i }),
    ).toBeInTheDocument();
  });

  it("copia al portapapeles cuando se clicka y muestra el check", async () => {
    const user = userEvent.setup();
    // Después de userEvent.setup() navigator.clipboard ya existe (jsdom
    // + polyfill de userEvent). Espiamos writeText sin reemplazarlo.
    const spy = vi.spyOn(navigator.clipboard, "writeText");

    render(
      <CopyToClipboardButton
        value="http://hubplay.local:8096"
        label="Copiar URL"
      />,
    );

    await user.click(screen.getByRole("button", { name: /copiar url/i }));

    expect(spy).toHaveBeenCalledWith("http://hubplay.local:8096");
    const btn = screen.getByTestId("copy-to-clipboard");
    await waitFor(() => {
      expect(btn.querySelector("svg.text-green-500")).not.toBeNull();
    });
  });

  it("silencia rechazos de writeText (insecure context, permission denied)", async () => {
    const user = userEvent.setup();
    const spy = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockRejectedValue(new Error("not allowed"));

    render(<CopyToClipboardButton value="x" label="Copiar" />);

    // El try/catch del componente debe tragar el rechazo sin propagar
    // ni crashear React.
    await user.click(screen.getByRole("button", { name: /copiar/i }));
    expect(spy).toHaveBeenCalled();
    const btn = screen.getByTestId("copy-to-clipboard");
    // Sin éxito en la copia, el check verde no aparece.
    expect(btn.querySelector("svg.text-green-500")).toBeNull();
  });
});
