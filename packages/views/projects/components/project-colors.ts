import type { CSSProperties } from "react";

export interface ProjectColorOption {
  value: string;
  labelKey:
    | "blue"
    | "indigo"
    | "purple"
    | "pink"
    | "red"
    | "orange"
    | "amber"
    | "emerald"
    | "teal"
    | "slate";
}

export const PROJECT_COLORS: ProjectColorOption[] = [
  { value: "#3B82F6", labelKey: "blue" },
  { value: "#6366F1", labelKey: "indigo" },
  { value: "#8B5CF6", labelKey: "purple" },
  { value: "#EC4899", labelKey: "pink" },
  { value: "#EF4444", labelKey: "red" },
  { value: "#F97316", labelKey: "orange" },
  { value: "#F59E0B", labelKey: "amber" },
  { value: "#10B981", labelKey: "emerald" },
  { value: "#14B8A6", labelKey: "teal" },
  { value: "#64748B", labelKey: "slate" },
];

const HEX_COLOR_RE = /^#[0-9a-fA-F]{6}$/;

/**
 * Returns a soft, theme-safe translucent background tint for issue titles
 * belonging to a colored project.
 */
export function projectTitleTintStyle(color?: string | null): CSSProperties | undefined {
  const trimmed = color?.trim();
  if (!trimmed || !HEX_COLOR_RE.test(trimmed)) return undefined;
  return {
    backgroundColor: `color-mix(in srgb, ${trimmed} 15%, transparent)`,
  };
}
