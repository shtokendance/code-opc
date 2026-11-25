"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  availableKnowledgeBasesOptions,
  projectKnowledgeBasesOptions,
  useBindProjectKnowledgeBase,
  useUnbindProjectKnowledgeBase,
} from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import type { AvailableKnowledgeBaseItem } from "@multica/core/types";
import { BookOpen, ChevronRight, Plus, X } from "@multica/ui/icons";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { toast } from "sonner";

export function ProjectKnowledgeSection({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(true);
  const [pickerOpen, setPickerOpen] = useState(false);

  const { data: bindings = [] } = useQuery(
    projectKnowledgeBasesOptions(wsId, projectId),
  );
  const { data: availableKBs = [], isLoading: loadingAvailable } = useQuery({
    ...availableKnowledgeBasesOptions(wsId),
    enabled: pickerOpen,
  });

  const bindMutation = useBindProjectKnowledgeBase(wsId, projectId);
  const unbindMutation = useUnbindProjectKnowledgeBase(wsId, projectId);

  const boundSet = new Set(bindings.map((b) => `${b.tenant_id}:${b.knowledge_base_id}`));
  const unlinkedKBs = availableKBs.filter(
    (kb) => kb.availability === "available" && !boundSet.has(`${kb.tenant_id}:${kb.knowledge_base_id}`),
  );

  const handleBind = async (kb: AvailableKnowledgeBaseItem) => {
    try {
      await bindMutation.mutateAsync({
        tenant_id: kb.tenant_id,
        knowledge_base_id: kb.knowledge_base_id,
        knowledge_base_name: kb.name,
      });
      setPickerOpen(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "关联知识库失败");
    }
  };

  const handleUnbind = async (bindingId: string, name: string) => {
    try {
      await unbindMutation.mutateAsync(bindingId);
      toast.success(`已取消关联「${name}」`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "取消关联失败");
    }
  };

  return (
    <div className="mt-6 border-t pt-4">
      <div className="flex items-center justify-between">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="flex items-center gap-1.5 text-caption font-medium text-muted-foreground hover:text-foreground transition-colors"
        >
          <ChevronRight
            className={`size-3.5 transition-transform ${open ? "rotate-90" : ""}`}
          />
          <BookOpen className="size-3.5" />
          <span>项目知识库</span>
          {bindings.length > 0 && (
            <span className="rounded bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
              {bindings.length}
            </span>
          )}
        </button>

        <Popover open={pickerOpen} onOpenChange={setPickerOpen}>
          <PopoverTrigger
            render={
              <button
                type="button"
                className="inline-flex h-6 items-center gap-1 rounded-md px-2 text-micro text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
              />
            }
          >
            <Plus className="size-3" />
            <span>关联知识库</span>
          </PopoverTrigger>
          <PopoverContent align="end" className="w-72 p-1.5">
            <div className="px-2 py-1 text-micro font-medium text-muted-foreground">
              选择要关联到本项目的知识库
            </div>
            <div className="mt-1 max-h-60 space-y-0.5 overflow-y-auto">
              {loadingAvailable ? (
                <div className="px-2 py-3 text-center text-micro text-muted-foreground">
                  正在加载知识库列表...
                </div>
              ) : unlinkedKBs.length === 0 ? (
                <div className="px-2 py-3 text-center text-micro text-muted-foreground">
                  暂无更多可关联的知识库
                </div>
              ) : (
                unlinkedKBs.map((kb) => (
                  <button
                    key={`${kb.tenant_id}:${kb.knowledge_base_id}`}
                    type="button"
                    disabled={bindMutation.isPending}
                    onClick={() => handleBind(kb)}
                    className="flex w-full flex-col rounded px-2 py-1.5 text-left hover:bg-accent transition-colors"
                  >
                    <span className="truncate text-caption font-medium text-foreground">
                      {kb.name}
                    </span>
                    <span className="truncate text-micro text-muted-foreground">
                      {kb.tenant_name || kb.tenant_id}
                    </span>
                  </button>
                ))
              )}
            </div>
          </PopoverContent>
        </Popover>
      </div>

      {open && (
        <div className="mt-3">
          {bindings.length > 0 ? (
            <div className="flex flex-wrap gap-1.5">
              {bindings.map((b) => (
                <span
                  key={b.id}
                  className="inline-flex items-center gap-1 rounded-md border bg-muted/50 px-2 py-1 text-micro text-foreground"
                >
                  <BookOpen className="size-3 text-muted-foreground" />
                  <span>{b.knowledge_base_name || b.knowledge_base_id}</span>
                  <button
                    type="button"
                    aria-label={`移除 ${b.knowledge_base_name || b.knowledge_base_id}`}
                    disabled={unbindMutation.isPending}
                    onClick={() =>
                      handleUnbind(b.id, b.knowledge_base_name || b.knowledge_base_id)
                    }
                    className="ml-0.5 rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
                  >
                    <X className="size-3" />
                  </button>
                </span>
              ))}
            </div>
          ) : (
            <p className="text-micro text-muted-foreground">
              暂未绑定项目知识库；关联后将在 Agent 执行本项目任务时自动带入。
            </p>
          )}
        </div>
      )}
    </div>
  );
}
