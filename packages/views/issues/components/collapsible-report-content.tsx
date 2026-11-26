import { memo, useState } from "react";
import type { Attachment } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { ChevronDown, ChevronUp } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { ReadonlyContent } from "../../editor";
import { useLocale, useT } from "../../i18n";

export const DEFAULT_REPORT_COLLAPSE_THRESHOLD = 1200;

export function formatReportCount(count: number, locale: string = "zh-Hans"): string {
  const isZh = locale.startsWith("zh");
  if (isZh) {
    if (count >= 10000) {
      const w = (count / 10000).toFixed(1).replace(/\.0$/, "");
      return `${w} 万字`;
    }
    return `${count.toLocaleString()} 字`;
  }
  if (count >= 1000) {
    const k = (count / 1000).toFixed(1).replace(/\.0$/, "");
    return `${k}k chars`;
  }
  return `${count} chars`;
}

export interface CollapsibleReportContentProps {
  content: string;
  attachments?: Attachment[];
  /**
   * If true, this content is treated as an agent report or task execution output.
   * Defaults to true if omitted.
   */
  isAgentOrTask?: boolean;
  /** Character threshold to trigger collapsible mode. Defaults to 1200. */
  threshold?: number;
  /** Title displayed in the side drawer header */
  title?: string;
  className?: string;
}

export const CollapsibleReportContent = memo(function CollapsibleReportContent({
  content,
  attachments,
  isAgentOrTask = true,
  threshold = DEFAULT_REPORT_COLLAPSE_THRESHOLD,
  className,
}: CollapsibleReportContentProps) {
  const { t } = useT("issues");
  const locale = useLocale();
  const [expanded, setExpanded] = useState(false);

  const shouldCollapse = isAgentOrTask && content.length >= threshold;

  if (!shouldCollapse) {
    return <ReadonlyContent content={content} attachments={attachments} className={className} />;
  }

  const formattedCount = formatReportCount(content.length, locale);

  return (
    <div className={cn("relative", className)}>
      <div
        className={cn(
          "relative transition-[max-height] duration-200",
          !expanded && "max-h-[420px] overflow-hidden"
        )}
      >
        <ReadonlyContent content={content} attachments={attachments} />
        {!expanded && (
          <div
            data-testid="report-gradient-mask"
            className="pointer-events-none absolute inset-x-0 bottom-0 h-32 bg-gradient-to-t from-background via-background/80 to-transparent"
          />
        )}
      </div>

      <div className="mt-2 flex flex-wrap items-center gap-2 pt-1">
        <Button
          variant="outline"
          size="xs"
          onClick={() => setExpanded(!expanded)}
          className="flex items-center gap-1.5 font-medium text-muted-foreground hover:text-foreground"
        >
          <span>
            {expanded
              ? t(($) => $.comment.collapse_report)
              : t(($) => $.comment.expand_report, { total: formattedCount })}
          </span>
          {expanded ? <ChevronUp className="size-3.5" /> : <ChevronDown className="size-3.5" />}
        </Button>
      </div>
    </div>
  );
});
