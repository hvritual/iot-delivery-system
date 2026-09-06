// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, describe, expect, it, vi } from "vitest";

import { DeliverySidebar } from "@/components/delivery-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";

beforeAll(() => {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
});

describe("DeliverySidebar", () => {
  it("organizes personal and workspace navigation, then routes a board into the item workspace", async () => {
    const user = userEvent.setup();
    const onSelectBoard = vi.fn();
    const onNavigate = vi.fn();

    render(
      <SidebarProvider>
        <DeliverySidebar
          activeBoard="研发交付效能"
          activeSurface="items"
          boards={[
            { board: "设备质量与连接", total: 1 },
            { board: "产品与平台能力", total: 0 },
            { board: "研发交付效能", total: 4 },
            { board: "运营保障与安全", total: 0 },
            { board: "客户与业务价值", total: 0 },
          ]}
          onNavigate={onNavigate}
          onSelectBoard={onSelectBoard}
          sampleVisible={false}
        />
      </SidebarProvider>,
    );

    expect(screen.getByText("我的工作")).toBeInTheDocument();
    expect(screen.getByText("工作空间")).toBeInTheDocument();
    expect(screen.getByText("五个交付板块")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /研发交付效能/ }),
    ).toHaveAttribute("data-active");
    await user.click(screen.getByRole("button", { name: /项目与排期/ }));
    expect(onNavigate).toHaveBeenCalledWith("projects");
    await user.click(screen.getByRole("button", { name: /设备质量与连接/ }));
    expect(onSelectBoard).toHaveBeenCalledWith("设备质量与连接");
    // Board selection owns the atomic navigation; do not prompt/discard twice.
    expect(onSelectBoard).toHaveBeenCalledTimes(1);
    expect(onNavigate).not.toHaveBeenCalledWith("items");
  });
});
