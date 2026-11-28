// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { fetchLatestRelease } from "./github-release";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("fetchLatestRelease", () => {
  it("fetches macOS and Windows desktop release URLs from the updates check API", async () => {
    const fetchMock = vi.fn(async (input: string | URL) => {
      const url = String(input);
      if (url.includes("platform=darwin")) {
        return new Response(
          JSON.stringify({
            available: true,
            latest_version: "0.13.7",
            pub_date: "2026-11-27T11:33:51.550Z",
            download_url: "/api/desktop/updates/download/CodeOPC-0.13.7-arm64.zip",
            filename: "CodeOPC-0.13.7-arm64.zip",
            size: 16580146,
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      return new Response(
        JSON.stringify({
          available: true,
          latest_version: "0.13.7",
          pub_date: "2026-11-27T11:33:51.550Z",
          download_url: "/api/desktop/updates/download/CodeOPC-0.13.7-win-x64.zip",
          filename: "CodeOPC-0.13.7-win-x64.zip",
          size: 17449009,
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await fetchLatestRelease();
    expect(result.version).toBe("0.13.7");
    expect(result.assets.macArm64Zip).toBe("/api/desktop/updates/download/CodeOPC-0.13.7-arm64.zip");
    expect(result.assets.winX64Zip).toBe("/api/desktop/updates/download/CodeOPC-0.13.7-win-x64.zip");
    expect(result.sizes.macArm64Zip).toBe(16580146);
    expect(result.sizes.winX64Zip).toBe(17449009);
  });

  it("returns an empty release shape when the API errors", async () => {
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("network down")));

    const result = await fetchLatestRelease();
    expect(result).toEqual({
      version: null,
      publishedAt: null,
      htmlUrl: null,
      assets: {},
      sizes: {},
    });
    expect(warnSpy).toHaveBeenCalled();
  });
});
