import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { KnowledgeSpaceSkillImportPanel } from "./knowledge-space-skill-import-panel";

const mocks = vi.hoisted(() => ({
  listKnowledgeSpaceSkills: vi.fn(),
  importKnowledgeSpaceSkill: vi.fn(),
  listSkills: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listKnowledgeSpaceSkills: mocks.listKnowledgeSpaceSkills,
    importKnowledgeSpaceSkill: mocks.importKnowledgeSpaceSkill,
    listSkills: mocks.listSkills,
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/platform", () => ({
  getCurrentSlug: () => "dept-bde",
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (fn: (dict: any) => string, vars?: Record<string, any>) => {
      const dict = {
        create: {
          local: {
            select_all: "Select all",
            cancel: "Cancel",
            import: "Import",
            import_count: `Import ${vars?.count ?? 0}`,
            importing: "Importing...",
            done: "Done",
            summary: `Success ${vars?.success ?? 0} · Failed ${vars?.failed ?? 0}`,
            fallback_error: "Import failed",
          },
          knowledge: {
            all_spaces: "All spaces",
            search_placeholder: "Search skill name or space...",
            refresh: "Refresh",
            load_failed: "Failed to load",
            retry: "Retry",
            empty: "No skills",
            empty_filtered: "No matching skills",
            partial_warning: "Partial failure",
            exists_badge: "Exists",
            not_ready: "Not ready",
            conflict_banner: `${vars?.count ?? 0} conflict(s)`,
            conflict_overwrite: "Overwrite",
            conflict_rename: "Rename",
            conflict_skip: "Skip",
            apply_resolution: "Apply & continue",
          },
        },
      };
      return fn(dict);
    },
  }),
}));

function renderPanel(onImported = vi.fn(), onBulkDone = vi.fn()) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return {
    onImported,
    onBulkDone,
    ...render(
      <QueryClientProvider client={qc}>
        <KnowledgeSpaceSkillImportPanel
          onImported={onImported}
          onBulkDone={onBulkDone}
        />
      </QueryClientProvider>,
    ),
  };
}

describe("KnowledgeSpaceSkillImportPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.listSkills.mockResolvedValue([]);
    mocks.listKnowledgeSpaceSkills.mockResolvedValue({
      items: [
        {
          resource_kind: "skill_catalog",
          tenant_id: "296492",
          tenant_name: "技术交付中心",
          catalog_id: "cat-1",
          name: "delivery-helper",
          description: "Delivery automation skill",
          version: "1.2.0",
          bundle_sha256: "abc123",
          availability: "available",
        },
        {
          resource_kind: "skill_catalog",
          tenant_id: "10001",
          tenant_name: "本来生活",
          catalog_id: "cat-2",
          name: "not-ready-skill",
          description: "Archive pending",
          availability: "not_ready",
          unavailable_reason: "ARCHIVE_NOT_READY",
        },
      ],
      total: 2,
      offset: 0,
      limit: 100,
      complete: true,
      failures: [],
    });
  });

  it("lists skills from accessible knowledge spaces and imports selected skill", async () => {
    const user = userEvent.setup();
    const createdSkill = { id: "skill-1", name: "delivery-helper" };
    mocks.importKnowledgeSpaceSkill.mockResolvedValue({
      status: "created",
      skill: createdSkill,
    });

    const { onImported } = renderPanel();

    expect(await screen.findByText("delivery-helper")).toBeInTheDocument();
    expect(screen.getByText("技术交付中心")).toBeInTheDocument();
    expect(screen.getByText("v1.2.0")).toBeInTheDocument();

    // Unavailable item should be disabled
    const notReadyBtn = screen.getByText("not-ready-skill").closest("button");
    expect(notReadyBtn).toBeDisabled();

    // Select and import available item
    await user.click(screen.getByText("delivery-helper").closest("button")!);
    await user.click(screen.getByRole("button", { name: /Import 1/i }));

    await waitFor(() => {
      expect(mocks.importKnowledgeSpaceSkill).toHaveBeenCalledWith(
        expect.objectContaining({
          catalog_id: "cat-1",
          tenant_id: "296492",
          expected_sha256: "abc123",
          tenant_name: "技术交付中心",
          on_conflict: "fail",
        }),
        { workspaceSlug: "dept-bde" },
      );
    });
    expect(onImported).toHaveBeenCalledWith(createdSkill);
  });

  it("filters skills by search query", async () => {
    const user = userEvent.setup();
    renderPanel();

    expect(await screen.findByText("delivery-helper")).toBeInTheDocument();
    expect(screen.getByText("not-ready-skill")).toBeInTheDocument();

    await user.type(screen.getByPlaceholderText("Search skill name or space..."), "delivery");
    expect(
      screen.getByText((_, node) => node?.tagName === "SPAN" && node?.textContent === "delivery-helper"),
    ).toBeInTheDocument();
    expect(screen.queryByText("not-ready-skill")).not.toBeInTheDocument();
  });
});
