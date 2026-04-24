import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ProgramDetailModal } from "./ProgramDetailModal";
import type { Channel, EPGProgram } from "@/api/types";

const NOW = new Date("2026-04-24T20:00:00Z").getTime();

function channel(overrides: Partial<Channel> = {}): Channel {
  return {
    id: "c1",
    library_id: "lib1",
    name: "La 1",
    number: 1,
    group: null,
    group_name: null,
    category: "general",
    logo_initials: "L1",
    logo_bg: "#111",
    logo_fg: "#fff",
    logo_url: null,
    stream_url: "http://stream/c1",
    language: "",
    country: "",
    is_active: true,
    ...overrides,
  };
}

function program(overrides: Partial<EPGProgram> = {}): EPGProgram {
  // Default: live now, started 30 min ago, 60 min long.
  return {
    id: "p1",
    channel_id: "c1",
    title: "Telediario",
    description: "Las noticias del día.",
    start_time: new Date(NOW - 30 * 60_000).toISOString(),
    end_time: new Date(NOW + 30 * 60_000).toISOString(),
    category: "news",
    icon_url: null,
    ...overrides,
  };
}

describe("ProgramDetailModal", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns null when not open", () => {
    const { container } = render(
      <ProgramDetailModal
        isOpen={false}
        onClose={vi.fn()}
        program={program()}
        channel={channel()}
        upNext={[]}
        onWatch={vi.fn()}
      />,
    );
    expect(container.textContent).toBe("");
  });

  it("returns null when program is missing even if isOpen=true", () => {
    // Defensive shape: the parent might transition through (open=true,
    // program=null) for a tick during a state cleanup. The modal must
    // not render an empty dialog.
    const { container } = render(
      <ProgramDetailModal
        isOpen={true}
        onClose={vi.fn()}
        program={null}
        channel={channel()}
        upNext={[]}
        onWatch={vi.fn()}
      />,
    );
    expect(container.textContent).toBe("");
  });

  it("renders title, channel name, time, duration and category", () => {
    render(
      <ProgramDetailModal
        isOpen={true}
        onClose={vi.fn()}
        program={program()}
        channel={channel()}
        upNext={[]}
        onWatch={vi.fn()}
      />,
    );
    // Modal sets title via aria-label on the dialog AND a heading.
    expect(screen.getByRole("dialog")).toHaveAttribute(
      "aria-label",
      "Telediario",
    );
    expect(screen.getByRole("heading", { level: 2 })).toHaveTextContent(
      "Telediario",
    );
    expect(screen.getByText("La 1")).toBeInTheDocument();
    // 60-minute duration.
    expect(screen.getByText(/60 min/)).toBeInTheDocument();
    // Category capitalised.
    expect(screen.getByText("News")).toBeInTheDocument();
  });

  it("shows EN VIVO badge for currently-airing programmes", () => {
    render(
      <ProgramDetailModal
        isOpen={true}
        onClose={vi.fn()}
        program={program()}
        channel={channel()}
        upNext={[]}
        onWatch={vi.fn()}
      />,
    );
    expect(screen.getByText("EN VIVO")).toBeInTheDocument();
  });

  it("hides EN VIVO badge for past or future programmes", () => {
    const past = program({
      start_time: new Date(NOW - 120 * 60_000).toISOString(),
      end_time: new Date(NOW - 60 * 60_000).toISOString(),
    });
    render(
      <ProgramDetailModal
        isOpen={true}
        onClose={vi.fn()}
        program={past}
        channel={channel()}
        upNext={[]}
        onWatch={vi.fn()}
      />,
    );
    expect(screen.queryByText("EN VIVO")).toBeNull();
  });

  it("renders the description verbatim with line breaks preserved", () => {
    const p = program({ description: "Line 1\n\nLine 2" });
    render(
      <ProgramDetailModal
        isOpen={true}
        onClose={vi.fn()}
        program={p}
        channel={channel()}
        upNext={[]}
        onWatch={vi.fn()}
      />,
    );
    // whitespace-pre-line preserves "\n"; the text node still
    // matches via a regex on the joined content.
    expect(screen.getByText(/Line 1/)).toBeInTheDocument();
    expect(screen.getByText(/Line 2/)).toBeInTheDocument();
  });

  it("falls back to a 'sin sinopsis' note when description is null", () => {
    const p = program({ description: null });
    render(
      <ProgramDetailModal
        isOpen={true}
        onClose={vi.fn()}
        program={p}
        channel={channel()}
        upNext={[]}
        onWatch={vi.fn()}
      />,
    );
    expect(screen.getByText(/sin sinopsis/i)).toBeInTheDocument();
  });

  it("renders the up-next list when entries are provided", () => {
    const next1 = program({
      id: "p2",
      title: "Documental",
      start_time: new Date(NOW + 30 * 60_000).toISOString(),
      end_time: new Date(NOW + 90 * 60_000).toISOString(),
    });
    const next2 = program({
      id: "p3",
      title: "Película",
      start_time: new Date(NOW + 90 * 60_000).toISOString(),
      end_time: new Date(NOW + 180 * 60_000).toISOString(),
    });
    render(
      <ProgramDetailModal
        isOpen={true}
        onClose={vi.fn()}
        program={program()}
        channel={channel()}
        upNext={[next1, next2]}
        onWatch={vi.fn()}
      />,
    );
    // Section heading present.
    expect(screen.getByRole("heading", { level: 3 })).toHaveTextContent(
      /a continuación/i,
    );
    expect(screen.getByText("Documental")).toBeInTheDocument();
    expect(screen.getByText("Película")).toBeInTheDocument();
  });

  it("hides the up-next section when the list is empty", () => {
    render(
      <ProgramDetailModal
        isOpen={true}
        onClose={vi.fn()}
        program={program()}
        channel={channel()}
        upNext={[]}
        onWatch={vi.fn()}
      />,
    );
    expect(screen.queryByRole("heading", { level: 3 })).toBeNull();
  });

  it("calls onWatch when the primary action is clicked", () => {
    const onWatch = vi.fn();
    render(
      <ProgramDetailModal
        isOpen={true}
        onClose={vi.fn()}
        program={program()}
        channel={channel()}
        upNext={[]}
        onWatch={onWatch}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /ver canal ahora/i }));
    expect(onWatch).toHaveBeenCalledTimes(1);
  });

  it("disables the watch button for ended programmes", () => {
    const ended = program({
      start_time: new Date(NOW - 120 * 60_000).toISOString(),
      end_time: new Date(NOW - 60 * 60_000).toISOString(),
    });
    render(
      <ProgramDetailModal
        isOpen={true}
        onClose={vi.fn()}
        program={ended}
        channel={channel()}
        upNext={[]}
        onWatch={vi.fn()}
      />,
    );
    const btn = screen.getByRole("button", { name: /ya terminó/i });
    expect(btn).toBeDisabled();
  });

  it("calls onClose when the close button is clicked", () => {
    const onClose = vi.fn();
    render(
      <ProgramDetailModal
        isOpen={true}
        onClose={onClose}
        program={program()}
        channel={channel()}
        upNext={[]}
        onWatch={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /cerrar/i }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
