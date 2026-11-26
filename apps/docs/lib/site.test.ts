import { beforeEach, describe, expect, it, vi } from "vitest";

const existingDocs = vi.hoisted(() => new Set<string>());

vi.mock("node:fs", () => ({
  existsSync: vi.fn((path: string) => {
    const normalized = path.replaceAll("\\", "/");
    return [...existingDocs].some((suffix) => normalized.endsWith(suffix));
  }),
}));

const pages = new Map<string, { url: string }>([
  ["en:", { url: "/" }],
  ["zh:", { url: "/zh" }],
  ["zh-Hant:", { url: "/zh-Hant" }],
  ["ko:", { url: "/ko" }],
  ["ja:", { url: "/ja" }],
  ["fr:", { url: "/fr" }],
  ["en:agents", { url: "/agents" }],
  ["zh:agents", { url: "/zh/agents" }],
  ["zh-Hant:agents", { url: "/zh-Hant/agents" }],
  ["ko:agents", { url: "/ko/agents" }],
  ["ja:agents", { url: "/ja/agents" }],
  ["fr:agents", { url: "/fr/agents" }],
]);

vi.mock("@/lib/source", () => ({
  source: {
    getPage: vi.fn((slugs: string[], lang: string) => {
      return pages.get(`${lang}:${slugs.join("/")}`) ?? null;
    }),
  },
}));

beforeEach(() => {
  existingDocs.clear();
  existingDocs.add("index.mdx");
  existingDocs.add("index.zh.mdx");
  existingDocs.add("agents.mdx");
  existingDocs.add("agents.zh.mdx");
});

describe("docsAlternates", () => {
  it("builds Chinese-only canonical and hreflang alternates for slug pages", async () => {
    const { docsAlternates } = await import("./site");

    expect(docsAlternates(["agents"])).toEqual({
      canonical: "https://www.multica.ai/docs/zh/agents",
      languages: {
        zh: "https://www.multica.ai/docs/zh/agents",
        "x-default": "https://www.multica.ai/docs/zh/agents",
      },
    });
  });

  it("omits non-zh hreflangs even when other locale MDX files exist", async () => {
    existingDocs.add("agents.ko.mdx");
    existingDocs.add("agents.ja.mdx");
    existingDocs.add("agents.zh-Hant.mdx");
    existingDocs.add("agents.fr.mdx");
    const { docsAlternates } = await import("./site");

    const { languages } = docsAlternates(["agents"]);
    expect(languages).not.toHaveProperty("ko");
    expect(languages).not.toHaveProperty("ja");
    expect(languages).not.toHaveProperty("zh-Hant");
    expect(languages).not.toHaveProperty("fr");
  });

  it("builds Chinese-only alternates for the docs root", async () => {
    const { docsAlternates } = await import("./site");

    expect(docsAlternates([])).toEqual({
      canonical: "https://www.multica.ai/docs/zh",
      languages: {
        zh: "https://www.multica.ai/docs/zh",
        "x-default": "https://www.multica.ai/docs/zh",
      },
    });
  });
});
