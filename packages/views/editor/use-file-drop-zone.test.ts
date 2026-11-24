import { describe, it, expect, vi } from "vitest";
import type React from "react";
import { renderHook, act } from "@testing-library/react";
import { useFileDropZone } from "./use-file-drop-zone";

describe("useFileDropZone", () => {
  function makeDragEvent(overrides: Partial<React.DragEvent<HTMLElement>> = {}): React.DragEvent<HTMLElement> {
    const defaultPrevented = overrides.defaultPrevented ?? false;
    let isPrevented = defaultPrevented;
    const dataTransfer = overrides.dataTransfer ?? ({
      types: ["Files"],
      files: [new File(["dummy content"], "test.png", { type: "image/png" })],
      dropEffect: "none",
    } as unknown as DataTransfer);

    return {
      preventDefault: vi.fn(() => {
        isPrevented = true;
      }),
      stopPropagation: vi.fn(),
      get defaultPrevented() {
        return isPrevented;
      },
      nativeEvent: {
        get defaultPrevented() {
          return isPrevented;
        },
      } as unknown as DragEvent,
      isDefaultPrevented: () => isPrevented,
      isPropagationStopped: () => false,
      persist: vi.fn(),
      dataTransfer,
      currentTarget: overrides.currentTarget ?? document.createElement("div"),
      target: overrides.target ?? document.createElement("div"),
      relatedTarget: overrides.relatedTarget ?? null,
      ...overrides,
    } as unknown as React.DragEvent<HTMLElement>;
  }

  it("activates isDragOver and sets dropEffect on dragenter with files", () => {
    const onDrop = vi.fn();
    const { result } = renderHook(() => useFileDropZone({ onDrop }));

    const event = makeDragEvent();
    act(() => {
      result.current.dropZoneProps.onDragEnter(event);
    });

    expect(event.preventDefault).toHaveBeenCalled();
    expect(event.dataTransfer?.dropEffect).toBe("copy");
    expect(result.current.isDragOver).toBe(true);
  });

  it("sets dropEffect = copy and calls preventDefault on dragover", () => {
    const onDrop = vi.fn();
    const { result } = renderHook(() => useFileDropZone({ onDrop }));

    const event = makeDragEvent();
    act(() => {
      result.current.dropZoneProps.onDragOver(event);
    });

    expect(event.preventDefault).toHaveBeenCalled();
    expect(event.dataTransfer?.dropEffect).toBe("copy");
  });

  it("handles nested dragenter and dragleave without prematurely clearing isDragOver", () => {
    const onDrop = vi.fn();
    const { result } = renderHook(() => useFileDropZone({ onDrop }));

    const enterEvent1 = makeDragEvent();
    act(() => {
      result.current.dropZoneProps.onDragEnter(enterEvent1);
    });
    expect(result.current.isDragOver).toBe(true);

    // Enter a child node
    const enterEvent2 = makeDragEvent();
    act(() => {
      result.current.dropZoneProps.onDragEnter(enterEvent2);
    });
    expect(result.current.isDragOver).toBe(true);

    // Leave a child node
    const leaveEvent1 = makeDragEvent();
    act(() => {
      result.current.dropZoneProps.onDragLeave(leaveEvent1);
    });
    // Still in the dropzone!
    expect(result.current.isDragOver).toBe(true);

    // Leave the parent dropzone completely
    const leaveEvent2 = makeDragEvent();
    act(() => {
      result.current.dropZoneProps.onDragLeave(leaveEvent2);
    });
    expect(result.current.isDragOver).toBe(false);
  });

  it("delivers files on drop and clears isDragOver", () => {
    const onDrop = vi.fn();
    const { result } = renderHook(() => useFileDropZone({ onDrop }));

    const file = new File(["data"], "doc.pdf", { type: "application/pdf" });
    const enterEvent = makeDragEvent();
    act(() => {
      result.current.dropZoneProps.onDragEnter(enterEvent);
    });
    expect(result.current.isDragOver).toBe(true);

    const dropEvent = makeDragEvent({
      dataTransfer: {
        types: ["Files"],
        files: [file],
      } as unknown as DataTransfer,
    });

    act(() => {
      result.current.dropZoneProps.onDrop(dropEvent);
    });

    expect(dropEvent.preventDefault).toHaveBeenCalled();
    expect(result.current.isDragOver).toBe(false);
    expect(onDrop).toHaveBeenCalledTimes(1);
    expect(onDrop.mock.calls[0]?.[0]?.[0]?.name).toBe("doc.pdf");
  });

  it("does not invoke onDrop if already handled by inner child (defaultPrevented)", () => {
    const onDrop = vi.fn();
    const { result } = renderHook(() => useFileDropZone({ onDrop }));

    const dropEvent = makeDragEvent({
      defaultPrevented: true,
      dataTransfer: {
        types: ["Files"],
        files: [new File(["data"], "doc.pdf")],
      } as unknown as DataTransfer,
    });

    act(() => {
      result.current.dropZoneProps.onDrop(dropEvent);
    });

    expect(onDrop).not.toHaveBeenCalled();
    expect(result.current.isDragOver).toBe(false);
  });

  it("clears isDragOver on document drop event", () => {
    const onDrop = vi.fn();
    const { result } = renderHook(() => useFileDropZone({ onDrop }));

    act(() => {
      result.current.dropZoneProps.onDragEnter(makeDragEvent());
    });
    expect(result.current.isDragOver).toBe(true);

    act(() => {
      document.dispatchEvent(new Event("drop"));
    });
    expect(result.current.isDragOver).toBe(false);
  });

  it("does not activate when enabled is false", () => {
    const onDrop = vi.fn();
    const { result } = renderHook(() => useFileDropZone({ onDrop, enabled: false }));

    act(() => {
      result.current.dropZoneProps.onDragEnter(makeDragEvent());
    });
    expect(result.current.isDragOver).toBe(false);

    act(() => {
      result.current.dropZoneProps.onDrop(makeDragEvent());
    });
    expect(onDrop).not.toHaveBeenCalled();
  });
});
