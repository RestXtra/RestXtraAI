import { ConstructionIcon } from "lucide-react";

import { Card, CardContent } from "@/components/ui/card";

// PlanningStub renders a "规划中 / 待接入" placeholder for pages whose backend
// capability is scaffolded but not yet wired end-to-end (per fusion blueprint §P4).
export function PlanningStub({
  title,
  description,
}: {
  title: string;
  description?: string;
}) {
  return (
    <div className="flex h-full items-center justify-center py-24">
      <Card className="w-full max-w-lg">
        <CardContent className="flex flex-col items-center gap-4 py-12 text-center">
          <ConstructionIcon className="size-12 text-muted-foreground" />
          <div className="space-y-1">
            <h3 className="text-lg font-semibold">{title}</h3>
            <p className="text-sm text-muted-foreground">
              {description ?? "该能力已完成表结构与接入脚手架，正在规划中，后续迭代落地。"}
            </p>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
