export interface AvailableKnowledgeBaseItem {
  resource_kind: string;
  tenant_id: string;
  tenant_name: string;
  source_tenant_id?: string;
  source_tenant_name?: string;
  permission?: string;
  knowledge_base_id: string;
  name: string;
  description: string;
  availability: string;
  unavailable_reason?: string;
  detail_url?: string;
}

export interface AvailableKnowledgeBaseListResponse {
  items: AvailableKnowledgeBaseItem[];
  total: number;
  offset: number;
  limit: number;
  complete: boolean;
}

export interface ProjectKnowledgeBase {
  id: string;
  workspace_id: string;
  project_id: string;
  tenant_id: string;
  knowledge_base_id: string;
  knowledge_base_name: string;
  created_by: string;
  created_at: string;
}

export interface ListProjectKnowledgeBasesResponse {
  knowledge_bases: ProjectKnowledgeBase[];
  total: number;
}

export interface BindProjectKnowledgeBaseRequest {
  tenant_id: string;
  knowledge_base_id: string;
  knowledge_base_name: string;
}
