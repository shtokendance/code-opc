"use client";

import { useEffect, useRef, useState, type ComponentType } from "react";
import Link from "@/features/landing/components/landing-link";
import { ArrowRight, ArrowUpRight } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { BorderBeam } from "@multica/ui/components/common/border-beam";
import {
  ActionLink,
  Container,
  actionClassName,
  Section,
  SectionHeader,
  focusRing,
} from "./primitives";
import { HERO_PROVIDERS } from "../components/provider-marks";
import { docsHrefForLocale, useLocale } from "../i18n";
import type { HomeDict } from "../i18n/types";
import { DownloadLink } from "../components/download/download-link";
import { OSIcon, OS_NAMES } from "../components/download/os-icons";
import { OtherDownloads } from "../components/download/other-downloads";
import { useDetectedPlatform } from "../components/download/use-detected-platform";
import { osOf, resolvePrimary } from "../utils/download-target";
import type { HomeDownload } from "../utils/home-download";
import { AutopilotsVisual, ChatVisual, RuntimesVisual, SkillsVisual, WorkflowVisual } from "./demo/feature-visuals";
import { HeroDemo } from "./demo/hero-demo";
import { PROVIDER_MARKS, useStoryLoop } from "./demo/mock-kit";
import { CtaBand, SitePage } from "./site-page";
import { SITE_PREVIEW } from "./sitemap";

export function HomePage({ download }: { download: HomeDownload }) {
  return (
    <SitePage>
      <Hero download={download} />
      <div className="site-rails">
        <Features />
        <Agents />
        <CtaBand />
      </div>
    </SitePage>
  );
}

/** The pill above the headline; it shows only while there is something to announce. */
const ANNOUNCEMENT: { label: string; href: string } | null = SITE_PREVIEW
  ? { label: "Announcement placeholder", href: "/blog" }
  : null;

function Hero({ download }: { download: HomeDownload }) {
  const { t } = useLocale();
  const copy = t.home.hero;
  const primary = resolvePrimary(useDetectedPlatform(download.platform), download.assets);
  const os = primary ? osOf(primary.key) : null;

  return (
    <section className="pt-site-hero pb-site-section">
      <Container className="max-w-site-wide">
        {ANNOUNCEMENT ? (
          <Link
            href={ANNOUNCEMENT.href}
            className={cn(
              "mb-5 inline-flex items-center gap-2 rounded-site-control border border-site-line bg-site-surface py-1 pr-3 pl-1 text-site-small text-site-ink-secondary transition-colors hover:border-site-line-strong",
              focusRing,
            )}
          >
            <span className="rounded-site-control bg-site-sunken px-2 py-0.5 text-site-ink">New</span>
            {ANNOUNCEMENT.label}
            <ArrowRight className="size-3.5 text-site-muted" aria-hidden />
          </Link>
        ) : null}
        <HeroHeadline />
        <p className="mt-3 max-w-[38rem] text-site-body text-site-muted text-pretty">{copy.lead}</p>
        <div className="mt-6 flex flex-wrap gap-2.5">
          {primary && os ? (
            <DownloadLink downloadKey={primary.key} href={primary.href} download className={actionClassName("primary", "lg")}>
              <OSIcon os={os} size={16} aria-hidden />
              {copy.downloadFor.replace("{os}", OS_NAMES[os])}
            </DownloadLink>
          ) : (
            <ActionLink href="/login" size="lg">
              {t.site.header.getStarted}
            </ActionLink>
          )}
        </div>
        {primary ? (
          <OtherDownloads primary={primary.key} assets={download.assets} sizes={download.sizes} version={download.version} />
        ) : null}
      </Container>
      <Container className="mt-10 max-w-site-wide lg:mt-12">
        <HeroDemo />
      </Container>
    </section>
  );
}

// Each name must fit the headline's line count on a 375px phone, or the page
// below jumps as the rotation reaches it — which rules out "DeepSeek Harness".
const HERO_AGENTS = [
  "Claude Code",
  "Codex",
  "Cursor",
  "Hermes",
  "OpenCode",
  "GitHub Copilot",
  "Antigravity",
  "Qwen Code",
  "Kimi CLI",
  "Pi",
] as const;
const HERO_AGENT_HOLDS = HERO_AGENTS.map(() => 3600);

function HeroHeadline() {
  const { t, locale } = useLocale();
  const { headlinePrefix, headlineSuffix } = t.home.hero;
  const { ref, step } = useStoryLoop(HERO_AGENT_HOLDS, 0);
  const measures = useRef<(HTMLSpanElement | null)[]>([]);
  const [widths, setWidths] = useState<number[] | null>(null);

  useEffect(() => {
    const items = measures.current.filter((el): el is HTMLSpanElement => el !== null);
    const measure = () => setWidths(items.map((el) => el.getBoundingClientRect().width));
    const resize = new ResizeObserver(measure);
    items.forEach((el) => resize.observe(el));
    return () => resize.disconnect();
  }, []);

  return (
    <div ref={ref}>
      <h1 className="font-site-display text-site-heading text-site-ink">
        <span className="sr-only">
          {headlinePrefix}
          {new Intl.ListFormat(locale, { type: "disjunction" }).format(HERO_AGENTS)}
          {headlineSuffix}
        </span>
        <span aria-hidden className="block text-balance">
          {headlinePrefix}
          <span className="relative inline-flex rounded-full bg-brand/5 px-[0.36em] [--border-beam-width:1.5px]">
            <span
              className="relative inline-grid overflow-hidden transition-[width] duration-500 ease-[cubic-bezier(0.22,1,0.36,1)] motion-reduce:transition-none"
              style={{ width: widths?.[step] }}
            >
              {HERO_AGENTS.map((name, index) => (
                <span
                  key={name}
                  ref={(el) => {
                    measures.current[index] = el;
                  }}
                  className={cn(
                    "col-start-1 row-start-1 inline-flex w-max items-center gap-[0.2em] whitespace-nowrap transition-[opacity,filter] ease-out motion-reduce:transition-none",
                    index === step ? "opacity-100 blur-none delay-150 duration-300" : "opacity-0 blur-[3px] duration-150",
                    // Until the names are measured the pill sizes to the first one, not the widest.
                    !widths && index !== step && "absolute top-0 left-0",
                  )}
                >
                  <ProviderMark name={name} />
                  {name}
                </span>
              ))}
            </span>
            <BorderBeam />
          </span>
          {headlineSuffix}
        </span>
      </h1>
    </div>
  );
}

function ProviderMark({ name }: { name: string }) {
  const Mark = PROVIDER_MARKS.get(name);
  return Mark ? <Mark className="size-[0.72em] shrink-0" /> : null;
}

type FeatureId = keyof HomeDict["features"]["items"];

const FEATURES: { id: FeatureId; visual: ComponentType }[] = [
  { id: "runtimes", visual: RuntimesVisual },
  { id: "workflows", visual: WorkflowVisual },
  { id: "chat", visual: ChatVisual },
  { id: "autopilots", visual: AutopilotsVisual },
  { id: "skills", visual: SkillsVisual },
];

function Features() {
  const { t } = useLocale();
  return (
    <Section>
      <Container>
        <SectionHeader title={t.home.features.title} />
        <div className="mt-site-block grid gap-x-site-grid-col gap-y-site-grid-row md:grid-cols-2">
          {FEATURES.slice(0, 2).map((feature) => (
            <FeatureCard key={feature.id} {...feature} />
          ))}
        </div>
        <div className="mt-site-grid-row grid gap-x-site-grid-col gap-y-site-grid-row md:grid-cols-3">
          {FEATURES.slice(2).map((feature) => (
            <FeatureCard key={feature.id} {...feature} />
          ))}
        </div>
      </Container>
    </Section>
  );
}

const POPULAR_AGENTS = ["Claude Code", "Codex", "Cursor", "GitHub Copilot", "OpenCode", "Hermes", "Antigravity", "Qwen Code"];

function Agents() {
  const agents = POPULAR_AGENTS.map((name) => HERO_PROVIDERS.find((provider) => provider.name === name)!);
  const { locale, t } = useLocale();
  const copy = t.home.agents;
  const docsHref = docsHrefForLocale(locale);
  return (
    <Section>
      <Container>
        <h2 className="max-w-[44rem] font-site-display text-site-heading text-balance">
          <span className="block text-site-muted">{copy.lead}</span>
          <span className="block text-site-ink">{copy.title}</span>
        </h2>
        <ul className="mt-site-block grid grid-cols-2 gap-y-8 md:grid-cols-4 md:gap-y-10">
          {agents.map(({ name, Mark, size }, index) => (
            <li
              key={name}
              className={cn(
                "flex flex-col gap-8 border-site-line px-4 md:gap-10 md:px-5",
                index % 2 === 1 && "border-l",
                index % 4 !== 0 && "md:border-l",
                index % 4 === 2 && "max-md:border-l-0",
              )}
            >
              <span className="flex size-7 items-center text-site-muted">
                <Mark className={size ?? "size-6"} />
              </span>
              <span className="text-site-body text-site-muted">{name}</span>
            </li>
          ))}
        </ul>
        <ActionLink href={`${docsHref}/providers`} tone="ghost" className="mt-8 ml-1 md:mt-10 md:ml-2">
          {copy.all}
          <ArrowUpRight className="size-4" aria-hidden />
        </ActionLink>
      </Container>
    </Section>
  );
}

function FeatureCard({ id, visual: Visual }: (typeof FEATURES)[number]) {
  const { title, body, alt } = useLocale().t.home.features.items[id];
  return (
    <div>
      <div role="img" aria-label={alt} data-frost={id} className="site-frost aspect-[4/3] rounded-site-panel">
        <Visual />
      </div>
      <h3 className="mt-4 text-site-card text-site-ink">{title}</h3>
      <p className="mt-site-stack max-w-[32rem] text-site-body text-site-muted">{body}</p>
    </div>
  );
}
