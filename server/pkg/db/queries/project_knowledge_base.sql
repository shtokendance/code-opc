-- name: ListProjectKnowledgeBases :many
SELECT * FROM project_knowledge_base
WHERE project_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: CreateProjectKnowledgeBase :one
INSERT INTO project_knowledge_base (
    workspace_id,
    project_id,
    tenant_id,
    knowledge_base_id,
    knowledge_base_name,
    created_by
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (project_id, tenant_id, knowledge_base_id)
DO UPDATE SET knowledge_base_name = EXCLUDED.knowledge_base_name
RETURNING *;

-- name: DeleteProjectKnowledgeBase :execrows
DELETE FROM project_knowledge_base
WHERE id = $1 AND project_id = $2 AND workspace_id = $3;

-- name: DeleteProjectKnowledgeBasesByProject :exec
DELETE FROM project_knowledge_base
WHERE project_id = $1 AND workspace_id = $2;
