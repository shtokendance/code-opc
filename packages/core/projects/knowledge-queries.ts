import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { projectKeys } from "./queries";
import type {
  BindProjectKnowledgeBaseRequest,
  ListProjectKnowledgeBasesResponse,
} from "../types";

export const projectKnowledgeBaseKeys = {
  list: (wsId: string, projectId: string) =>
    [...projectKeys.detail(wsId, projectId), "knowledge-bases"] as const,
  available: (wsId: string) => ["workspace", wsId, "knowledge-bases", "available"] as const,
};

export function projectKnowledgeBasesOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: projectKnowledgeBaseKeys.list(wsId, projectId),
    queryFn: () => api.listProjectKnowledgeBases(projectId),
    select: (data) => data.knowledge_bases,
  });
}

export function availableKnowledgeBasesOptions(wsId: string) {
  return queryOptions({
    queryKey: projectKnowledgeBaseKeys.available(wsId),
    queryFn: () => api.listAvailableKnowledgeBases({ limit: 100 }),
    select: (data) => data.items,
  });
}

export function useBindProjectKnowledgeBase(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: BindProjectKnowledgeBaseRequest) =>
      api.bindProjectKnowledgeBase(projectId, data),
    onSuccess: (created) => {
      qc.setQueryData<ListProjectKnowledgeBasesResponse>(
        projectKnowledgeBaseKeys.list(wsId, projectId),
        (old) =>
          old && !old.knowledge_bases.some((kb) => kb.id === created.id)
            ? {
                ...old,
                knowledge_bases: [...old.knowledge_bases, created],
                total: old.total + 1,
              }
            : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: projectKnowledgeBaseKeys.list(wsId, projectId),
      });
    },
  });
}

export function useUnbindProjectKnowledgeBase(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (bindingId: string) =>
      api.unbindProjectKnowledgeBase(projectId, bindingId),
    onSuccess: (_data, bindingId) => {
      qc.setQueryData<ListProjectKnowledgeBasesResponse>(
        projectKnowledgeBaseKeys.list(wsId, projectId),
        (old) =>
          old
            ? {
                ...old,
                knowledge_bases: old.knowledge_bases.filter((kb) => kb.id !== bindingId),
                total: Math.max(0, old.total - 1),
              }
            : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: projectKnowledgeBaseKeys.list(wsId, projectId),
      });
    },
  });
}
