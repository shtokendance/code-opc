"use client";

import { useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  AlertTriangle,
  BookOpen,
  CheckCircle2,
  Download,
  Loader2,
  RefreshCw,
  Search,
  SkipForward,
} from "@multica/ui/icons";
import type {
  KnowledgeSpaceSkillItem,
  Skill,
  SkillSummary,
} from "@multica/core/types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { getCurrentSlug } from "@multica/core/platform";
import {
  skillDetailOptions,
  skillListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Progress } from "@multica/ui/components/ui/progress";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { HighlightText } from "../../search/highlight-text";

type ImportResult = {
  key: string;
  item: KnowledgeSpaceSkillItem;
  status: "created" | "updated" | "conflict" | "skipped" | "failed";
  existingName?: string;
  canOverwrite?: boolean;
  error?: string;
  skill?: Skill;
};

type BulkState = {
  phase: "idle" | "importing" | "resolving" | "done";
  total: number;
  completed: number;
  results: ImportResult[];
};

type ConflictAction = "overwrite" | "rename" | "skip";

type ConflictResolution = {
  action: ConflictAction;
  renameName: string;
};

const INITIAL_STATE: BulkState = {
  phase: "idle",
  total: 0,
  completed: 0,
  results: [],
};

function itemKey(item: KnowledgeSpaceSkillItem): string {
  return `${item.tenant_id}:${item.catalog_id}`;
}

function ResultIcon({ status }: { status: ImportResult["status"] }) {
  switch (status) {
    case "created":
      return <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-green-600" />;
    case "updated":
      return <RefreshCw className="h-3.5 w-3.5 shrink-0 text-blue-600" />;
    case "conflict":
      return <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-amber-600" />;
    case "skipped":
      return <SkipForward className="h-3.5 w-3.5 shrink-0 text-yellow-600" />;
    case "failed":
      return <AlertCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />;
  }
}

export function KnowledgeSpaceSkillImportPanel({
  onImported,
  onBulkDone,
}: {
  onImported: (skill: Skill) => void;
  onBulkDone: () => void;
}) {
  const { t } = useT("skills");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);

  const [selectedSpace, setSelectedSpace] = useState<string>("all");
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(() => new Set());
  const [bulk, setBulk] = useState<BulkState>(INITIAL_STATE);
  const [conflictChoices, setConflictChoices] = useState<Record<string, ConflictResolution>>({});

  const {
    data,
    isLoading,
    isError,
    error,
    refetch,
    isFetching,
  } = useQuery({
    queryKey: ["knowledge-space-skills", wsId],
    queryFn: () => api.listKnowledgeSpaceSkills({ limit: 100 }),
    staleTime: 30_000,
  });

  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));

  const existingSkillNames = useMemo(() => {
    const map = new Map<string, SkillSummary>();
    for (const s of workspaceSkills) {
      map.set(s.name.toLowerCase(), s);
    }
    return map;
  }, [workspaceSkills]);

  const items = useMemo(() => data?.items ?? [], [data?.items]);

  const spaces = useMemo(() => {
    const map = new Map<string, string>();
    for (const it of items) {
      if (!map.has(it.tenant_id)) {
        map.set(it.tenant_id, it.tenant_name || it.tenant_id);
      }
    }
    return Array.from(map.entries()).map(([id, name]) => ({ id, name }));
  }, [items]);

  const spaceSelectItems = useMemo(
    () => [
      { value: "all", label: t(($) => $.create.knowledge.all_spaces) },
      ...spaces.map((sp) => ({ value: sp.id, label: sp.name })),
    ],
    [spaces, t],
  );

  const filteredItems = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    return items.filter((it) => {
      if (selectedSpace !== "all" && it.tenant_id !== selectedSpace) {
        return false;
      }
      if (q) {
        const matchName = it.name.toLowerCase().includes(q);
        const matchDesc = (it.description ?? "").toLowerCase().includes(q);
        const matchTenant = (it.tenant_name ?? "").toLowerCase().includes(q);
        if (!matchName && !matchDesc && !matchTenant) return false;
      }
      return true;
    });
  }, [items, selectedSpace, searchQuery]);

  const availableFiltered = useMemo(
    () =>
      filteredItems.filter(
        (it) => it.availability === "available" && Boolean(it.bundle_sha256),
      ),
    [filteredItems],
  );

  const allFilteredSelected =
    availableFiltered.length > 0 &&
    availableFiltered.every((it) => selectedKeys.has(itemKey(it)));

  const toggleItem = (key: string) => {
    setSelectedKeys((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const toggleAllFiltered = () => {
    if (allFilteredSelected) {
      setSelectedKeys((prev) => {
        const next = new Set(prev);
        for (const it of availableFiltered) next.delete(itemKey(it));
        return next;
      });
    } else {
      setSelectedKeys((prev) => {
        const next = new Set(prev);
        for (const it of availableFiltered) next.add(itemKey(it));
        return next;
      });
    }
  };

  const invalidateWorkspaceSkills = (importedSkills: Skill[]) => {
    for (const skill of importedSkills) {
      qc.setQueryData(skillDetailOptions(wsId, skill.id).queryKey, skill);
    }
    qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
  };

  const runImport = async () => {
    const toImport = items.filter(
      (it) =>
        selectedKeys.has(itemKey(it)) &&
        it.availability === "available" &&
        Boolean(it.bundle_sha256),
    );
    if (toImport.length === 0) return;

    const slug = getCurrentSlug();
    setBulk({
      phase: "importing",
      total: toImport.length,
      completed: 0,
      results: [],
    });

    const results: ImportResult[] = [];
    const createdSkills: Skill[] = [];

    for (const item of toImport) {
      const key = itemKey(item);
      try {
        const res = await api.importKnowledgeSpaceSkill(
          {
            catalog_id: item.catalog_id,
            tenant_id: item.tenant_id,
            expected_sha256: item.bundle_sha256 ?? "",
            tenant_name: item.tenant_name,
            detail_url: item.detail_url,
            on_conflict: "fail",
          },
          slug ? { workspaceSlug: slug } : undefined,
        );

        if (res.status === "created" && res.skill) {
          createdSkills.push(res.skill);
          results.push({ key, item, status: "created", skill: res.skill });
        } else if (res.status === "updated" && res.skill) {
          createdSkills.push(res.skill);
          results.push({ key, item, status: "updated", skill: res.skill });
        } else if (res.status === "conflict") {
          results.push({
            key,
            item,
            status: "conflict",
            existingName: res.existing_skill?.name ?? item.name,
            canOverwrite: res.existing_skill?.can_overwrite ?? false,
          });
        } else if (res.status === "skipped") {
          results.push({ key, item, status: "skipped" });
        } else {
          results.push({
            key,
            item,
            status: "failed",
            error: res.reason || t(($) => $.create.local.fallback_error),
          });
        }
      } catch (err) {
        results.push({
          key,
          item,
          status: "failed",
          error: err instanceof Error ? err.message : t(($) => $.create.local.fallback_error),
        });
      }

      setBulk({
        phase: "importing",
        total: toImport.length,
        completed: results.length,
        results: [...results],
      });
    }

    if (createdSkills.length > 0) {
      invalidateWorkspaceSkills(createdSkills);
    }

    const conflicts = results.filter((r) => r.status === "conflict");
    if (conflicts.length > 0) {
      const defaults: Record<string, ConflictResolution> = {};
      for (const c of conflicts) {
        defaults[c.key] = {
          action: c.canOverwrite ? "overwrite" : "rename",
          renameName: `${c.item.name} copy`,
        };
      }
      setConflictChoices(defaults);
      setBulk({
        phase: "resolving",
        total: toImport.length,
        completed: results.length,
        results,
      });
      return;
    }

    if (
      toImport.length === 1 &&
      results[0]?.skill &&
      (results[0].status === "created" || results[0].status === "updated")
    ) {
      onImported(results[0].skill);
      return;
    }

    setBulk({
      phase: "done",
      total: toImport.length,
      completed: results.length,
      results,
    });
  };

  const resolveConflicts = async () => {
    const conflicts = bulk.results.filter((r) => r.status === "conflict");
    if (conflicts.length === 0) return;

    const slug = getCurrentSlug();
    setBulk((prev) => ({ ...prev, phase: "importing" }));

    const nextResults = [...bulk.results];
    const resolvedSkills: Skill[] = [];

    for (const c of conflicts) {
      const choice = conflictChoices[c.key] ?? { action: "skip", renameName: `${c.item.name} copy` };
      const idx = nextResults.findIndex((r) => r.key === c.key);
      if (choice.action === "skip") {
        if (idx >= 0) nextResults[idx] = { ...c, status: "skipped" };
        continue;
      }

      try {
        const res = await api.importKnowledgeSpaceSkill(
          {
            catalog_id: c.item.catalog_id,
            tenant_id: c.item.tenant_id,
            expected_sha256: c.item.bundle_sha256 ?? "",
            tenant_name: c.item.tenant_name,
            detail_url: c.item.detail_url,
            name: choice.action === "rename" ? choice.renameName.trim() || `${c.item.name} copy` : undefined,
            on_conflict: choice.action,
          },
          slug ? { workspaceSlug: slug } : undefined,
        );

        if ((res.status === "created" || res.status === "updated") && res.skill) {
          resolvedSkills.push(res.skill);
          if (idx >= 0) {
            nextResults[idx] = { ...c, status: res.status, skill: res.skill };
          }
        } else {
          if (idx >= 0) {
            nextResults[idx] = {
              ...c,
              status: "failed",
              error: res.reason || t(($) => $.create.local.fallback_error),
            };
          }
        }
      } catch (err) {
        if (idx >= 0) {
          nextResults[idx] = {
            ...c,
            status: "failed",
            error: err instanceof Error ? err.message : t(($) => $.create.local.fallback_error),
          };
        }
      }
    }

    if (resolvedSkills.length > 0) {
      invalidateWorkspaceSkills(resolvedSkills);
    }

    setBulk((prev) => ({
      ...prev,
      phase: "done",
      results: nextResults,
    }));
  };

  if (bulk.phase === "resolving") {
    const conflicts = bulk.results.filter((r) => r.status === "conflict");
    return (
      <>
        <div
          ref={scrollRef}
          style={fadeStyle}
          className="flex-1 min-h-0 space-y-3 overflow-y-auto px-5 py-4"
        >
          <div className="flex items-center gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-caption text-amber-700 dark:text-amber-300">
            <AlertTriangle className="h-4 w-4 shrink-0" />
            <span>{t(($) => $.create.knowledge.conflict_banner, { count: conflicts.length })}</span>
          </div>

          <div className="space-y-2.5">
            {conflicts.map((c) => {
              const choice = conflictChoices[c.key] ?? {
                action: c.canOverwrite ? "overwrite" : "rename",
                renameName: `${c.item.name} copy`,
              };
              return (
                <div key={c.key} className="space-y-2 rounded-md border p-3">
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate font-medium text-body-sm">{c.item.name}</span>
                    <Badge variant="outline" className="shrink-0 text-caption">
                      {c.item.tenant_name || c.item.tenant_id}
                    </Badge>
                  </div>
                  <div className="flex flex-wrap gap-1.5">
                    {c.canOverwrite && (
                      <Button
                        type="button"
                        size="xs"
                        variant={choice.action === "overwrite" ? "default" : "outline"}
                        onClick={() =>
                          setConflictChoices((prev) => ({
                            ...prev,
                            [c.key]: { ...choice, action: "overwrite" },
                          }))
                        }
                      >
                        {t(($) => $.create.knowledge.conflict_overwrite)}
                      </Button>
                    )}
                    <Button
                      type="button"
                      size="xs"
                      variant={choice.action === "rename" ? "default" : "outline"}
                      onClick={() =>
                        setConflictChoices((prev) => ({
                          ...prev,
                          [c.key]: { ...choice, action: "rename" },
                        }))
                      }
                    >
                      {t(($) => $.create.knowledge.conflict_rename)}
                    </Button>
                    <Button
                      type="button"
                      size="xs"
                      variant={choice.action === "skip" ? "default" : "outline"}
                      onClick={() =>
                        setConflictChoices((prev) => ({
                          ...prev,
                          [c.key]: { ...choice, action: "skip" },
                        }))
                      }
                    >
                      {t(($) => $.create.knowledge.conflict_skip)}
                    </Button>
                  </div>
                  {choice.action === "rename" && (
                    <Input
                      value={choice.renameName}
                      onChange={(e) =>
                        setConflictChoices((prev) => ({
                          ...prev,
                          [c.key]: { ...choice, renameName: e.target.value },
                        }))
                      }
                      className="h-8 text-body-sm"
                    />
                  )}
                </div>
              );
            })}
          </div>
        </div>
        <div className="flex shrink-0 items-center justify-end gap-2 border-t px-5 py-3">
          <Button type="button" variant="outline" size="sm" onClick={onBulkDone}>
            {t(($) => $.create.local.cancel)}
          </Button>
          <Button type="button" size="sm" onClick={resolveConflicts}>
            {t(($) => $.create.knowledge.apply_resolution)}
          </Button>
        </div>
      </>
    );
  }

  if (bulk.phase === "importing" || bulk.phase === "done") {
    const successCount = bulk.results.filter(
      (r) => r.status === "created" || r.status === "updated",
    ).length;
    const failedCount = bulk.results.filter((r) => r.status === "failed").length;
    const pct = bulk.total > 0 ? Math.round((bulk.completed / bulk.total) * 100) : 0;

    return (
      <>
        <div
          ref={scrollRef}
          style={fadeStyle}
          className="flex-1 min-h-0 space-y-3 overflow-y-auto px-5 py-4"
        >
          <div className="space-y-1.5">
            <div className="flex items-center justify-between text-caption text-muted-foreground">
              <span>
                {bulk.phase === "importing"
                  ? t(($) => $.create.local.importing)
                  : t(($) => $.create.local.summary, {
                      success: successCount,
                      failed: failedCount,
                    })}
              </span>
              <span>
                {bulk.completed} / {bulk.total}
              </span>
            </div>
            <Progress value={pct} className="h-1.5" />
          </div>

          <div className="space-y-1.5">
            {bulk.results.map((r) => (
              <div
                key={r.key}
                className="flex items-start gap-2.5 rounded-md border px-3 py-2 text-body-sm"
              >
                <ResultIcon status={r.status} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">{r.skill?.name ?? r.item.name}</span>
                    <Badge variant="outline" className="shrink-0 text-caption">
                      {r.item.tenant_name || r.item.tenant_id}
                    </Badge>
                  </div>
                  {r.error && (
                    <p className="mt-0.5 text-caption text-destructive">{r.error}</p>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
        <div className="flex shrink-0 items-center justify-end gap-2 border-t px-5 py-3">
          <Button
            type="button"
            size="sm"
            disabled={bulk.phase === "importing"}
            onClick={onBulkDone}
          >
            {bulk.phase === "importing" ? (
              <>
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                {t(($) => $.create.local.importing)}
              </>
            ) : (
              t(($) => $.create.local.done)
            )}
          </Button>
        </div>
      </>
    );
  }

  return (
    <>
      {/* Toolbar: Space filter + Search */}
      <div className="flex shrink-0 items-center gap-2 border-b px-5 py-2.5">
        <Select
          items={spaceSelectItems}
          value={selectedSpace}
          onValueChange={(v) => setSelectedSpace(v ?? "all")}
        >
          <SelectTrigger className="h-8 w-44 shrink-0 text-body-sm">
            <SelectValue placeholder={t(($) => $.create.knowledge.all_spaces)} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">
              {t(($) => $.create.knowledge.all_spaces)}
            </SelectItem>
            {spaces.map((sp) => (
              <SelectItem key={sp.id} value={sp.id}>
                {sp.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder={t(($) => $.create.knowledge.search_placeholder)}
            className="h-8 pl-8 text-body-sm"
          />
        </div>

        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          onClick={() => refetch()}
          disabled={isFetching}
          aria-label={t(($) => $.create.knowledge.refresh)}
        >
          <RefreshCw className={cn("h-3.5 w-3.5", isFetching && "animate-spin")} />
        </Button>
      </div>

      {/* Scrollable list */}
      <div
        ref={scrollRef}
        style={fadeStyle}
        className="flex-1 min-h-0 overflow-y-auto px-5 py-3"
      >
        {isLoading ? (
          <div className="space-y-2.5">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-14 w-full rounded-md" />
            ))}
          </div>
        ) : isError ? (
          <div className="flex flex-col items-center justify-center gap-2.5 py-12 text-center">
            <AlertCircle className="h-8 w-8 text-destructive/80" />
            <p className="text-body-sm font-medium">
              {t(($) => $.create.knowledge.load_failed)}
            </p>
            <p className="max-w-sm text-caption text-muted-foreground">
              {error instanceof Error ? error.message : String(error ?? "")}
            </p>
            <Button type="button" variant="outline" size="sm" onClick={() => refetch()}>
              <RefreshCw className="h-3.5 w-3.5" />
              {t(($) => $.create.knowledge.retry)}
            </Button>
          </div>
        ) : filteredItems.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-12 text-center">
            <BookOpen className="h-8 w-8 text-muted-foreground/50" />
            <p className="text-body-sm text-muted-foreground">
              {items.length === 0
                ? t(($) => $.create.knowledge.empty)
                : t(($) => $.create.knowledge.empty_filtered)}
            </p>
          </div>
        ) : (
          <div className="space-y-1.5">
            {data && !data.complete && data.failures.length > 0 && (
              <div className="mb-2 flex items-center gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-caption text-amber-700 dark:text-amber-300">
                <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                <span>{t(($) => $.create.knowledge.partial_warning)}</span>
              </div>
            )}

            {filteredItems.map((item) => {
              const key = itemKey(item);
              const available =
                item.availability === "available" && Boolean(item.bundle_sha256);
              const checked = selectedKeys.has(key);
              const alreadyExists = existingSkillNames.has(item.name.toLowerCase());

              return (
                <button
                  key={key}
                  type="button"
                  disabled={!available}
                  onClick={() => toggleItem(key)}
                  className={cn(
                    "flex w-full items-start gap-3 rounded-md border px-3 py-2.5 text-left transition-colors",
                    !available
                      ? "cursor-not-allowed opacity-50"
                      : checked
                        ? "border-primary/40 bg-accent/60"
                        : "hover:bg-accent/40",
                  )}
                >
                  <Checkbox
                    checked={checked}
                    disabled={!available}
                    onCheckedChange={() => toggleItem(key)}
                    onClick={(e) => e.stopPropagation()}
                    className="mt-0.5"
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="truncate font-medium text-body-sm">
                        <HighlightText text={item.name} query={searchQuery} />
                      </span>
                      {item.version && (
                        <Badge variant="secondary" className="text-caption">
                          {`v${item.version}`}
                        </Badge>
                      )}
                      <Badge variant="outline" className="text-caption">
                        {item.tenant_name || item.tenant_id}
                      </Badge>
                      {alreadyExists && (
                        <Badge variant="outline" className="border-amber-500/40 text-caption text-amber-600">
                          {t(($) => $.create.knowledge.exists_badge)}
                        </Badge>
                      )}
                      {!available && (
                        <Badge variant="outline" className="text-caption text-muted-foreground">
                          {t(($) => $.create.knowledge.not_ready)}
                        </Badge>
                      )}
                    </div>
                    {item.description && (
                      <p className="mt-0.5 line-clamp-2 text-caption text-muted-foreground">
                        <HighlightText text={item.description} query={searchQuery} />
                      </p>
                    )}
                  </div>
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* Footer */}
      <div className="flex shrink-0 items-center justify-between gap-2 border-t px-5 py-3">
        <div className="flex items-center gap-2">
          {availableFiltered.length > 0 && (
            <label className="flex cursor-pointer items-center gap-2 text-caption text-muted-foreground">
              <Checkbox
                checked={allFilteredSelected}
                onCheckedChange={toggleAllFiltered}
              />
              <span>{t(($) => $.create.local.select_all)}</span>
            </label>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Button type="button" variant="outline" size="sm" onClick={onBulkDone}>
            {t(($) => $.create.local.cancel)}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={selectedKeys.size === 0}
            onClick={runImport}
          >
            <Download className="h-3.5 w-3.5" />
            {selectedKeys.size > 0
              ? t(($) => $.create.local.import_count, { count: selectedKeys.size })
              : t(($) => $.create.local.import)}
          </Button>
        </div>
      </div>
    </>
  );
}
