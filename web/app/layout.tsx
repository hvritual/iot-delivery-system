import type { Metadata } from "next";
import { Inter } from "next/font/google";
import type { ReactNode } from "react";

import { TooltipProvider } from "@/components/ui/tooltip";

import "./globals.css";

const deliverySans = Inter({
  display: "swap",
  subsets: ["latin"],
  variable: "--font-delivery-sans",
});

export const metadata: Metadata = {
  title: "IoT Delivery | 研发交付系统",
  description: "面向 IoT 软件研发交付的本地执行工作台。",
};

export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html className={deliverySans.variable} lang="zh-CN">
      <body>
        <TooltipProvider delay={150}>{children}</TooltipProvider>
      </body>
    </html>
  );
}
