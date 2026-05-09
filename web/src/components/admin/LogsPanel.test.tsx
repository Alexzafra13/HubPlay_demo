// LogsPanel — focuses on the pause / un-pause state machine
// because the previous shape (sync ref + useEffect drain) had
// real lint issues that masked subtle bugs. The current shape
// puts the drain in the click handler; this test pins that.
//
// EventSource is mocked so the test owns the message stream.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
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

  it("filter='ERROR' hides INFO + WARN entries from the view", () => {
    render(<LogsPanel />);
    emitInfo("info one");
    const es = MockEventSource.instances[0];
    act(() => {
      es.emit({ ts: new Date().toISOString(), level: "WARN", msg: "warn one" });
      es.emit({ ts: new Date().toISOString(), level: "ERROR", msg: "error one" });
    });

    // Switch the dropdown to "Solo Error".
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "ERROR" } });

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
});
