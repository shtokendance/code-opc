import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import {
  CollapsibleReportContent,
  formatReportCount,
} from "./collapsible-report-content";

vi.mock("../../editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <div data-testid="readonly-content">{content}</div>,
}));

describe("formatReportCount", () => {
  it("formats Chinese counts correctly below and above 10,000", () => {
    expect(formatReportCount(500, "zh-Hans")).toBe("500 字");
    expect(formatReportCount(1250, "zh-Hans")).toBe("1,250 字");
    expect(formatReportCount(20000, "zh-Hans")).toBe("2 万字");
    expect(formatReportCount(26135, "zh-Hans")).toBe("2.6 万字");
    expect(formatReportCount(26135, "zh-Hant")).toBe("2.6 万字");
  });

  it("formats English counts correctly below and above 1,000", () => {
    expect(formatReportCount(500, "en")).toBe("500 chars");
    expect(formatReportCount(1000, "en")).toBe("1k chars");
    expect(formatReportCount(2500, "en")).toBe("2.5k chars");
    expect(formatReportCount(26135, "en")).toBe("26.1k chars");
  });
});

describe("CollapsibleReportContent", () => {
  it("renders content directly when below threshold", () => {
    const shortContent = "Short task output summary.";
    renderWithI18n(<CollapsibleReportContent content={shortContent} threshold={100} />);

    expect(screen.getByTestId("readonly-content")).toHaveTextContent(shortContent);
    expect(screen.queryByTestId("report-gradient-mask")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /展开/ })).not.toBeInTheDocument();
  });

  it("renders collapsed preview with gradient mask and expand button when exceeding threshold (zh-Hans)", () => {
    const longContent = "A".repeat(26135);
    renderWithI18n(
      <CollapsibleReportContent
        content={longContent}
        threshold={1000}
        title="Agent Report"
      />,
      { locale: "zh-Hans" }
    );

    // Initial state: collapsed
    expect(screen.getByTestId("report-gradient-mask")).toBeInTheDocument();

    const expandBtn = screen.getByRole("button", { name: /2\.6 万字/ });
    expect(expandBtn).toBeInTheDocument();

    const drawerBtn = screen.getByRole("button", { name: /侧边抽屉/ });
    expect(drawerBtn).toBeInTheDocument();

    // Click to expand in-place
    fireEvent.click(expandBtn);

    // Gradient mask removed, button switches to collapse
    expect(screen.queryByTestId("report-gradient-mask")).not.toBeInTheDocument();
    const collapseBtn = screen.getByRole("button", { name: /收起报告/ });
    expect(collapseBtn).toBeInTheDocument();

    // Click to collapse back
    fireEvent.click(collapseBtn);
    expect(screen.getByTestId("report-gradient-mask")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /2\.6 万字/ })).toBeInTheDocument();
  });

  it("renders collapsed preview with English copy by default", () => {
    const longContent = "A".repeat(26135);
    renderWithI18n(
      <CollapsibleReportContent
        content={longContent}
        threshold={1000}
        title="Agent Report"
      />
    );

    expect(screen.getByRole("button", { name: /26\.1k chars/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /side drawer/ })).toBeInTheDocument();
  });

  it("opens side drawer on drawer button click", () => {
    const longContent = "Long report markdown body ".repeat(200);
    renderWithI18n(
      <CollapsibleReportContent
        content={longContent}
        threshold={500}
        title="Custom Report Title"
      />,
      { locale: "zh-Hans" }
    );

    const drawerBtn = screen.getByRole("button", { name: /侧边抽屉/ });
    fireEvent.click(drawerBtn);

    // Side drawer sheet dialog opens
    expect(screen.getByRole("heading", { name: "Custom Report Title" })).toBeInTheDocument();
  });
});
