import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act, fireEvent } from "@testing-library/react";
import "@/i18n";
import { UpNextOverlay } from "./UpNextOverlay";

describe("UpNextOverlay", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders the next episode title and S/E code", () => {
    render(
      <UpNextOverlay
        nextUp={{ title: "The Pilot", seasonNumber: 2, episodeNumber: 5 }}
        onPlayNow={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    expect(screen.getByText("The Pilot")).toBeInTheDocument();
    expect(screen.getByText("S2 · E5")).toBeInTheDocument();
  });

  it("calls onPlayNow when the timer expires", () => {
    const onPlayNow = vi.fn();
    render(
      <UpNextOverlay
        nextUp={{ title: "Foo" }}
        durationSeconds={1}
        onPlayNow={onPlayNow}
        onCancel={vi.fn()}
      />,
    );
    expect(onPlayNow).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(1100);
    });
    expect(onPlayNow).toHaveBeenCalledOnce();
  });

  it("calls onPlayNow immediately when the user clicks the play button", () => {
    const onPlayNow = vi.fn();
    render(
      <UpNextOverlay
        nextUp={{ title: "Foo" }}
        durationSeconds={5}
        onPlayNow={onPlayNow}
        onCancel={vi.fn()}
      />,
    );
    // The play button is the first one (autoFocus); cancel is the second.
    fireEvent.click(screen.getAllByRole("button")[0]);
    expect(onPlayNow).toHaveBeenCalledOnce();
  });

  it("calls onCancel when the user clicks the cancel button", () => {
    const onCancel = vi.fn();
    render(
      <UpNextOverlay
        nextUp={{ title: "Foo" }}
        durationSeconds={5}
        onPlayNow={vi.fn()}
        onCancel={onCancel}
      />,
    );
    fireEvent.click(screen.getByLabelText(/cancel|cancelar/i));
    expect(onCancel).toHaveBeenCalledOnce();
  });

  it("calls onCancel on Escape key", () => {
    const onCancel = vi.fn();
    render(
      <UpNextOverlay
        nextUp={{ title: "Foo" }}
        durationSeconds={5}
        onPlayNow={vi.fn()}
        onCancel={onCancel}
      />,
    );
    const event = new KeyboardEvent("keydown", { key: "Escape" });
    act(() => {
      document.dispatchEvent(event);
    });
    expect(onCancel).toHaveBeenCalledOnce();
  });

  it("does not fire onPlayNow if cancelled before the timer expires", () => {
    const onPlayNow = vi.fn();
    const onCancel = vi.fn();
    const { unmount } = render(
      <UpNextOverlay
        nextUp={{ title: "Foo" }}
        durationSeconds={5}
        onPlayNow={onPlayNow}
        onCancel={onCancel}
      />,
    );
    act(() => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    });
    // Parent typically unmounts the overlay in response to onCancel.
    unmount();
    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(onPlayNow).not.toHaveBeenCalled();
  });

  it("renders with no episode code when season/episode are null", () => {
    render(
      <UpNextOverlay
        nextUp={{ title: "Movie Sequel", seasonNumber: null, episodeNumber: null }}
        onPlayNow={vi.fn()}
        onCancel={vi.fn()}
      />,
    );
    // The episode code element shouldn't be rendered.
    expect(screen.queryByText(/S\d/)).toBeNull();
    expect(screen.getByText("Movie Sequel")).toBeInTheDocument();
  });
});
