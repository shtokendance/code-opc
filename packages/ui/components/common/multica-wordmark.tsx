import { cn } from "../../lib/utils";
import { ICON_SQUIRCLE } from "../../lib/codeopc-brand";

interface MulticaWordmarkProps extends React.ComponentProps<"svg"> {
  /**
   * Write delay in ms (preserved for compatibility).
   */
  writeDelayMs?: number;
  /** Accessible name */
  title?: string;
}

/**
 * The CodeOPC wordmark (icon badge + brand text).
 * Colour follows `currentColor`; height follows `className`.
 */
export function MulticaWordmark({
  writeDelayMs,
  title = "CodeOPC",
  className,
  ...props
}: MulticaWordmarkProps) {
  return (
    <svg
      viewBox="0 0 160 36"
      role="img"
      aria-label={title}
      className={cn("inline-block h-[1em] w-auto shrink-0 overflow-visible", className)}
      {...props}
    >
      <image href={ICON_SQUIRCLE} x="0" y="2" width="32" height="32" />
      <text
        x="40"
        y="25"
        fill="currentColor"
        fontSize="21"
        fontWeight="700"
        fontFamily="inherit"
        letterSpacing="-0.03em"
      >
        CodeOPC
      </text>
    </svg>
  );
}
