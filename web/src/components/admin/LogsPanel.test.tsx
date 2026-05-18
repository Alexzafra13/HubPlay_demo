// LogsPanel — focuses on the pause / un-pause state machine
// because the previous shape (sync ref + useEffect drain) had
// real lint issues that masked subtle bugs. The current shape
// puts the drain in the click handler; this test pins that.
//
// EventSource is mocked so the test owns the message stream.

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  act,
  cleanup,
} from "@testing-library/react";
import "@/i18n";

class MockEventSource {
  static instances: MockEventSource[] = [];
  url: string;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((ev: MessageEvent<string>) => void) | null = null;
  closed = false;
  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }
  close() {
    this.closed = true;
  }
  emit(payload: object) {
    this.onmessage?.({ data: JSON.stringify(payload) } as MessageEvent<string>);
  }
}

beforeEach(() => {
  MockEventSource.instances = [];
  // @ts-expect-error — replacing the global for the test
  globalThis.EventSource = MockEventSource;
});

afterEach(() => {
  cleanup();
});

import { LogsPanel } from "./LogsPanel";

function emitInfo(message: string) {
  const es = MockEventSource.instances[0];
  act(() => {
    es.emit({
      ts: new Date().toISOString(),
      level: "INFO",
      msg: message,
    });
  });
}

describe("LogsPanel pause / drain", () => {
  it("renders incoming entries while live", () => {
    render(<LogsPanel />);
    emitInfo("hello world");
    expect(screen.getByText("hello world")).toBeInTheDocument();
  });

  it("buffers entries while paused and drains them on un-pause", () => {
    render(<LogsPanel />);
    // Pause first
    fireEvent.click(screen.getByTitle(/pausar/i));

    // Emit two entries while paused — neither should appear yet.
    emitInfo("first while paused");
    emitInfo("second while paused");
    expect(screen.queryByText("first while paused")).toBeNull();
    expect(screen.queryByText("second while paused")).toBeNull();

    // The "N entradas en cola" hint is now visible.
    expect(screen.getByText(/2 entrada/i)).toBeInTheDocument();

    // Un-pause → both entries should land in one frame.
    fireEvent.click(screen.getByTitle(/reanudar/i));
    expect(screen.getByText("first while paused")).toBeInTheDocument();
    expect(screen.getByText("second while paused")).toBeInTheDocument();
  });

  it("clear button wipes the on-screen entries", () => {
    render(<LogsPanel />);
    emitInfo("first");
    emitInfo("second");
    expect(screen.getByText("first")).toBeInTheDocument();
    fireEvent.click(screen.getByTitle(/limpiar/i));
    expect(screen.queryByText("first")).toBeNull();
    expect(screen.queryByText("second")).toBeNull();
  });

  it("toggling INFO/WARN off leaves only ERROR entries visible", () => {
    render(<LogsPanel />);
    emitInfo("info one");
    const es = MockEventSource.instances[0];
    act(() => {
      es.emit({ ts: new Date().toISOString(), level: "WARN", msg: "warn one" });
      es.emit({ ts: new Date().toISOString(), level: "ERROR", msg: "error one" });
    });

    // El nuevo filter UI son toggle buttons por level en vez de un
    // dropdown. Por defecto DEBUG esta off, INFO/WARN/ERROR on. Para
    // dejar "solo ERROR" desactivamos INFO y WARN.
    fireEvent.click(screen.getByRole("button", { name: /^INFO$/i, pressed: true }));
    fireEvent.click(screen.getByRole("button", { name: /^WARN$/i, pressed: true }));

    expect(screen.queryByText("info one")).toBeNull();
    expect(screen.queryByText("warn one")).toBeNull();
    expect(screen.getByText("error one")).toBeInTheDocument();
  });

  it("flips the connected pill when EventSource fires onerror", () => {
    render(<LogsPanel />);
    const es = MockEventSource.instances[0];
    act(() => es.onopen?.());
    expect(screen.getByText(/^en vivo$/i)).toBeInTheDocument();
    act(() => es.onerror?.());
    expect(screen.getByText(/reconectando/i)).toBeInTheDocument();
  });

  // El search box filtra cliente-side substring sobre msg + module
  // + attrs. Es el feature mas pedido del rediseño - pin con un
  // test para que no se rompa silenciosamente.
  it("search filters entries by substring over message + attrs", () => {
    render(<LogsPanel />);
    const es = MockEventSource.instances[0];
    act(() => {
      es.emit({
        ts: new Date().toISOString(),
        level: "INFO",
        msg: "user logged in",
        attrs: { module: "auth", username: "alice" },
      });
      es.emit({
        ts: new Date().toISOString(),
        level: "INFO",
        msg: "library scan completed",
        attrs: { module: "scanner" },
      });
    });
    // Ambos visibles antes del filtro.
    expect(screen.getByText("user logged in")).toBeInTheDocument();
    expect(screen.getByText("library scan completed")).toBeInTheDocument();

    // Search "alice" -> solo el de auth.
    fireEvent.change(screen.getByPlaceholderText(/buscar/i), {
      target: { value: "alice" },
    });
    expect(screen.getByText("user logged in")).toBeInTheDocument();
    expect(screen.queryByText("library scan completed")).toBeNull();
  });

  // Click en chip de modulo activa el filtro - cero entries de
  // modulos distintos al clicado.
  it("clicking a module chip filters entries to that module", () => {
    render(<LogsPanel />);
    const es = MockEventSource.instances[0];
    act(() => {
      es.emit({
        ts: new Date().toISOString(),
        level: "INFO",
        msg: "user logged in",
        attrs: { module: "auth" },
      });
      es.emit({
        ts: new Date().toISOString(),
        level: "INFO",
        msg: "scan started",
        attrs: { module: "scanner" },
      });
    });

    // Click el chip "auth" (boton dentro de un <li> con texto "auth").
    // Hay 2 botones con texto "auth": el chip del toolbar arriba y
    // el chip inline en la entrada. Cualquiera de los dos activa.
    const authChips = screen.getAllByRole("button", { name: /^auth$/ });
    expect(authChips.length).toBeGreaterThan(0);
    fireEvent.click(authChips[0]);

    expect(screen.getByText("user logged in")).toBeInTheDocument();
    expect(screen.queryByText("scan started")).toBeNull();
  });
});
