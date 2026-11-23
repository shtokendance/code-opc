"use client";

import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowUpRight,
  CircleHelp,
  Megaphone,
  MessageCircle,
  Star,
} from "@multica/ui/icons";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { useModalStore } from "@multica/core/modals";
import { useConfigStore } from "@multica/core/config";
import { formatStarCount, githubStarsOptions, MULTICA_GITHUB_URL } from "@multica/core/github/stars";
import { createLogger } from "@multica/core/logger";
import { markStarPromptStarred } from "@multica/core/star-prompt";
import { GitHubIcon } from "../onboarding/components/brand-icons";
import { useT } from "../i18n";
import { RAIL_ICON_BUTTON_CLASS } from "./rail-styles";
import { useWhatsNewMenuPanel, WhatsNewMenuPanel } from "../whats-new/whats-new-menu-panel";

const logger = createLogger("help-launcher");

export function HelpLauncher() {
  const { t } = useT("layout");
  const serverVersion = useConfigStore((state) => state.serverVersion);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const whatsNew = useWhatsNewMenuPanel();
  const [open, setOpen] = useState(false);
  // Asked only once the menu opens, so closed sessions never call GitHub.
  const { data: stars } = useQuery({ ...githubStarsOptions(), enabled: open });
  return (
    <>
      <DropdownMenu open={open} onOpenChange={setOpen}>
        <TooltipHint label={t(($) => $.help.trigger)} side="right">
          <DropdownMenuTrigger
            ref={triggerRef}
            aria-label={t(($) => $.help.trigger)}
            className={RAIL_ICON_BUTTON_CLASS}
          >
            <CircleHelp />
          </DropdownMenuTrigger>
        </TooltipHint>
        <DropdownMenuContent
          align="end"
          side="right"
          sideOffset={8}
          className="min-w-40 max-w-56"
        >
          {whatsNew.available && (
            <DropdownMenuItem onClick={whatsNew.show}>
              <Megaphone className="h-3.5 w-3.5" />
              {t(($) => $.whats_new.trigger)}
              {whatsNew.newestUnread && (
                <span
                  data-slot="whats-new-tag"
                  className="ml-auto text-caption font-medium text-brand tabular-nums"
                >
                  {`v${whatsNew.newestUnread}`}
                </span>
              )}
            </DropdownMenuItem>
          )}
          <DropdownMenuItem
            render={
              <a href={MULTICA_GITHUB_URL} target="_blank" rel="noopener noreferrer" />
            }
            onClick={() => {
              markStarPromptStarred().catch((err) => logger.warn("failed to record star click", err));
            }}
          >
            <GitHubIcon className="h-3.5 w-3.5" />
            {t(($) => $.help.star_on_github)}
            {typeof stars === "number" ? (
              <span className="ml-auto inline-flex shrink-0 items-center gap-1 text-caption whitespace-nowrap text-muted-foreground tabular-nums">
                <Star aria-hidden="true" className="size-3" />
                {formatStarCount(stars)}
              </span>
            ) : (
              <ArrowUpRight className="size-3 translate-y-px text-faint-foreground" />
            )}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => useModalStore.getState().open("feedback")}
          >
            <MessageCircle className="h-3.5 w-3.5" />
            {t(($) => $.help.feedback)}
          </DropdownMenuItem>
          {serverVersion && (
            <>
              <DropdownMenuSeparator />
              {/* DropdownMenuLabel renders Base UI's Menu.GroupLabel, which reads
                  a Menu.Group context and throws if it has no Group ancestor. It
                  must always be wrapped in a DropdownMenuGroup — without it the
                  Help menu crashes the whole app on open (no error boundary sits
                  above the rail). */}
              <DropdownMenuGroup>
                <DropdownMenuLabel className="font-normal break-words">
                  {t(($) => $.help.server_version, { version: serverVersion })}
                </DropdownMenuLabel>
              </DropdownMenuGroup>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      <WhatsNewMenuPanel panel={whatsNew} anchor={triggerRef} />
    </>
  );
}
