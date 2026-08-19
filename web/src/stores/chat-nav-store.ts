"use client";

import { create } from "zustand";

// 会话列表与对话页共享状态：
//   selectedId —— 当前选中会话（0/null = 新建对话态）
//   bump      —— 自增信号，会话数据变化后通知侧栏列表刷新
// 设计为独立 store（非 provider），静态导出 + 客户端导航下两处订阅同一实例。
export type ChatNavState = {
  selectedId: number | null;
  bump: number;
  select: (id: number | null) => void;
  refresh: () => void;
};

export const useChatNavStore = create<ChatNavState>()((set) => ({
  selectedId: null,
  bump: 0,
  select: (id) => set({ selectedId: id }),
  refresh: () => set((s) => ({ bump: s.bump + 1 })),
}));
