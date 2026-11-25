CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_project_knowledge_base_unique
    ON project_knowledge_base (project_id, tenant_id, knowledge_base_id);
