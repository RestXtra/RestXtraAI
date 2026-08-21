"use client";

import * as React from "react";

import {
  Background,
  BackgroundVariant,
  Controls,
  type Edge as FlowEdge,
  type Node as FlowNode,
  MarkerType,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import { api } from "@/lib/api";
import type { Edge, TaskNode } from "@/lib/types";

const NODE_COLORS: Record<string, string> = {
  begin: "#64748b",
  goal: "#10b981",
  intent: "#3b82f6",
  fact: "#f59e0b",
  finding: "#e11d48",
  hint: "#8b5cf6",
};

function summary(node: TaskNode) {
  try {
    const payload = JSON.parse(node.payload ?? "") as Record<string, unknown>;
    for (const key of ["summary", "text", "description", "vulnclass"]) {
      if (typeof payload[key] === "string" && payload[key]) return payload[key] as string;
    }
  } catch {
    // Non-JSON payloads are valid for older exploration nodes.
  }
  return node.payload ? node.payload : `${node.type} #${node.id}`;
}

function layout(nodes: TaskNode[], edges: Edge[]) {
  const ids = new Set(nodes.map((node) => node.id));
  const depth = new Map(nodes.map((node) => [node.id, 0]));
  const incoming = new Map(nodes.map((node) => [node.id, 0]));
  const outgoing = new Map<string, string[]>();
  for (const edge of edges) {
    if (!ids.has(edge.src) || !ids.has(edge.dst)) continue;
    incoming.set(edge.dst, (incoming.get(edge.dst) ?? 0) + 1);
    outgoing.set(edge.src, [...(outgoing.get(edge.src) ?? []), edge.dst]);
  }
  const queue = nodes.filter((node) => incoming.get(node.id) === 0).map((node) => node.id);
  for (let index = 0; index < queue.length; index++) {
    const id = queue[index];
    for (const next of outgoing.get(id) ?? []) {
      depth.set(next, Math.max(depth.get(next) ?? 0, (depth.get(id) ?? 0) + 1));
      incoming.set(next, (incoming.get(next) ?? 0) - 1);
      if (incoming.get(next) === 0) queue.push(next);
    }
  }
  const rows = new Map<number, number>();
  return nodes.map((node) => {
    const column = depth.get(node.id) ?? 0;
    const row = rows.get(column) ?? 0;
    rows.set(column, row + 1);
    const color = NODE_COLORS[node.type] ?? "#64748b";
    return {
      id: node.id,
      position: { x: column * 300, y: row * 130 },
      data: { label: summary(node) },
      style: {
        width: 230,
        borderColor: color,
        borderWidth: 2,
        borderRadius: 8,
        background: "var(--card)",
        color: "var(--card-foreground)",
        fontSize: 12,
      },
    } satisfies FlowNode;
  });
}

export function FindingLineage({ findingId }: { findingId: string }) {
  const [nodes, setNodes] = React.useState<FlowNode[]>([]);
  const [edges, setEdges] = React.useState<FlowEdge[]>([]);
  const [loaded, setLoaded] = React.useState(false);

  React.useEffect(() => {
    let alive = true;
    void api
      .findingLineage(findingId)
      .then((graph) => {
        if (!alive) return;
        setNodes(layout(graph.nodes ?? [], graph.edges ?? []));
        setEdges(
          (graph.edges ?? []).map((edge, index) => ({
            id: `${index}-${edge.src}-${edge.dst}`,
            source: edge.src,
            target: edge.dst,
            label: edge.rel,
            labelShowBg: false,
            labelStyle: { fontSize: 10, fill: "#64748b" },
            style: { stroke: "#94a3b8", strokeWidth: 1.5 },
            markerEnd: { type: MarkerType.ArrowClosed, color: "#94a3b8" },
          })),
        );
      })
      .catch(() => {
        if (!alive) return;
        setNodes([]);
        setEdges([]);
      })
      .finally(() => {
        if (alive) setLoaded(true);
      });
    return () => {
      alive = false;
    };
  }, [findingId]);

  if (loaded && nodes.length === 0) {
    return <div className="p-8 text-center text-muted-foreground text-sm">该漏洞没有可用的探索链路。</div>;
  }

  return (
    <div className="h-[65vh] min-h-[480px] overflow-hidden border bg-background">
      <ReactFlowProvider>
        <ReactFlow nodes={nodes} edges={edges} fitView minZoom={0.25} maxZoom={1.6} nodesDraggable={false}>
          <Background variant={BackgroundVariant.Dots} gap={20} size={1} />
          <MiniMap pannable zoomable nodeColor={(node) => String(node.style?.borderColor ?? "#64748b")} />
          <Controls showInteractive={false} />
        </ReactFlow>
      </ReactFlowProvider>
    </div>
  );
}
