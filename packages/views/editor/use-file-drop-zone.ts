import { useState, useEffect, useCallback, useRef, type DragEvent } from "react";

interface UseFileDropZoneOptions {
  onDrop: (files: File[]) => void;
  enabled?: boolean;
}

function hasFiles(e: DragEvent): boolean {
  const types = e.dataTransfer?.types;
  if (!types) return false;
  const list = Array.from(types);
  return (
    list.includes("Files") ||
    list.includes("application/x-moz-file") ||
    list.includes("text/uri-list") ||
    list.some((t) => t.toLowerCase() === "files")
  );
}

function useFileDropZone({ onDrop, enabled = true }: UseFileDropZoneOptions) {
  const [isDragOver, setIsDragOver] = useState(false);
  const dragCounterRef = useRef(0);
  const onDropRef = useRef(onDrop);
  onDropRef.current = onDrop;

  // Clear on any document-level drop or dragend (e.g. user drops outside the zone)
  useEffect(() => {
    if (!enabled) return;
    const clear = () => {
      dragCounterRef.current = 0;
      setIsDragOver(false);
    };
    document.addEventListener("drop", clear);
    document.addEventListener("dragend", clear);
    return () => {
      document.removeEventListener("drop", clear);
      document.removeEventListener("dragend", clear);
    };
  }, [enabled]);

  const handleDragEnter = useCallback(
    (e: DragEvent) => {
      e.preventDefault();
      if (!enabled || !hasFiles(e)) return;
      dragCounterRef.current++;
      if (e.dataTransfer) {
        e.dataTransfer.dropEffect = "copy";
      }
      setIsDragOver(true);
    },
    [enabled],
  );

  const handleDragOver = useCallback((e: DragEvent) => {
    e.preventDefault();
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = "copy";
    }
  }, []);

  const handleDragLeave = useCallback((e: DragEvent) => {
    e.preventDefault();
    dragCounterRef.current--;
    if (dragCounterRef.current <= 0) {
      dragCounterRef.current = 0;
      setIsDragOver(false);
    }
  }, []);

  const handleDrop = useCallback(
    (e: DragEvent) => {
      const alreadyHandled = e.nativeEvent.defaultPrevented;
      e.preventDefault();
      dragCounterRef.current = 0;
      setIsDragOver(false);
      if (alreadyHandled || !enabled) return;
      const files = e.dataTransfer?.files;
      if (files?.length) {
        onDropRef.current(Array.from(files));
      }
    },
    [enabled],
  );

  const dropZoneProps = {
    onDragEnter: handleDragEnter,
    onDragOver: handleDragOver,
    onDragLeave: handleDragLeave,
    onDrop: handleDrop,
  };

  return { isDragOver: enabled && isDragOver, dropZoneProps };
}

export { useFileDropZone };

