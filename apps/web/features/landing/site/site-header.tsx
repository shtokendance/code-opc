"use client";

import { useEffect, useRef, useState } from "react";
import Link from "@/features/landing/components/landing-link";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { docsHrefForLocale, useLocale } from "../i18n";
import { useDashboardCtaHref } from "../utils/use-dashboard-cta";
import { MulticaWordmark } from "@multica/ui/components/common/multica-wordmark";
import { ActionLink, focusRing } from "./primitives";

export function SiteHeader() {
  const { locale, t } = useLocale();
  const labels = t.site.header;
  const user = useAuthStore((s) => s.user);
  const ctaHref = useDashboardCtaHref();
  const docsHref = docsHrefForLocale(locale);

  const [scrolled, setScrolled] = useState(false);
  const sentinelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const sentinel = sentinelRef.current;
    if (!sentinel) return;
    const observer = new IntersectionObserver(([entry]) =>
      setScrolled(!entry?.isIntersecting),
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, []);

  return (
    <>
      <div ref={sentinelRef} aria-hidden className="h-0" />
      <header
        className={cn(
          "sticky top-0 z-40 border-b bg-site-canvas transition-colors",
          scrolled ? "border-site-line" : "border-transparent",
        )}
      >
        <div className="mx-auto flex h-site-header w-full max-w-site-wide items-center justify-between gap-4 px-site-gutter">
          <Link
            href="/"
            aria-label={labels.home}
            className={cn("flex shrink-0 items-center rounded-sm", focusRing)}
          >
            <MulticaWordmark aria-hidden className="h-6 text-site-ink" />
          </Link>

          <div className="flex shrink-0 items-center gap-1.5">
            {user ? null : (
              <ActionLink href="/login" tone="secondary">
                {labels.logIn}
              </ActionLink>
            )}
            <ActionLink href={user ? ctaHref : docsHref}>{user ? labels.dashboard : labels.getStarted}</ActionLink>
          </div>
        </div>
      </header>
    </>
  );
}
