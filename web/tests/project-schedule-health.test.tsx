// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";

import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { ProjectScheduleHealth } from "@/src/components/ProjectScheduleHealth.jsx";

afterEach(cleanup);

describe("ProjectScheduleHealth", () => {
  it("explains schedule risks and remaining owner capacity for a selected project", () => {
    render(
      <ProjectScheduleHealth
        schedule={{
          projectId: "PRJ-1",
          totalItems: 6,
          scheduledItems: 4,
          unscheduledItems: 2,
          blockedItems: 1,
          overdueItems: 1,
          dependencyBlockedItems: 2,
          capacity: [
            {
              owner: "发布负责人",
              remainingEstimatePoints: 5,
              blockedItems: 0,
            },
          ],
          risks: [
            {
              itemId: "IOT-1",
              title: "灰度发布",
              owner: "发布负责人",
              dueDate: "2026-09-02",
              reason: "已逾期",
            },
          ],
        }}
      />,
    );

    expect(
      screen.getByRole("region", { name: "项目交付健康" }),
    ).toBeInTheDocument();
    expect(screen.getByText("已逾期")).toBeInTheDocument();
    expect(screen.getByText("依赖阻塞").parentElement).toHaveTextContent("2");
    expect(screen.getByText("未排期事项").parentElement).toHaveTextContent("2");
    expect(screen.getByText("5.0 pts")).toBeInTheDocument();
    expect(screen.getByText("发布负责人")).toBeInTheDocument();
    expect(screen.getByText(/未配置容量阈值/)).toBeInTheDocument();
    expect(screen.getByText("灰度发布")).toBeInTheDocument();
  });

  it("guides the user to select a project before a schedule is available", () => {
    render(<ProjectScheduleHealth schedule={null} />);

    expect(
      screen.getByText("选择项目后查看排期、依赖与成员剩余估算。"),
    ).toBeInTheDocument();
  });
});
