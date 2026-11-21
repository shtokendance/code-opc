package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/platformclient"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

func startClaimFixture(t *testing.T, status string) (string, string, time.Time) {
	t.Helper()
	if testHandler == nil {
		t.Fatal("PostgreSQL is required for start claim tests")
	}
	var agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id=$1 LIMIT 1`, testWorkspaceID).Scan(&agentID)
	issueID := dbfx.Issue(t, "start claim ownership")
	generation := time.Date(2026, 9, 17, 9, 0, 0, 123456000, time.UTC)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": issueID, "runtime_id": testRuntimeID,
		"status": status, "dispatched_at": generation,
	})
	return taskID, testRuntimeID, generation
}

func startClaimRequest(taskID, runtimeID string, generation time.Time) *http.Request {
	return withURLParam(newOwnerDaemonRequest("POST", "/api/daemon/tasks/"+taskID+"/start", map[string]string{
		"runtime_id": runtimeID, "dispatched_at": generation.Format(time.RFC3339Nano),
	}), "taskId", taskID)
}

func TestStartClaimLostResponseAndOwnership(t *testing.T) {
	for _, state := range []string{"dispatched", "waiting_local_directory"} {
		t.Run(state, func(t *testing.T) {
			id, runtimeID, generation := startClaimFixture(t, state)
			// A stale generation on the same runtime cannot win the FIRST start.
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation.Add(-time.Microsecond))).Want(http.StatusConflict)
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, "00000000-0000-0000-0000-000000000001", generation)).Want(http.StatusConflict)
			// Commit the first request, then discard its response (lost acknowledgement).
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation)).Want(http.StatusOK)
			var before pgtype.Timestamptz
			dbfx.QueryRow(t, `SELECT started_at FROM agent_task_queue WHERE id=$1`, id).Scan(&before)
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation)).Want(http.StatusOK)
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation.Add(-time.Microsecond))).Want(http.StatusConflict)
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, "00000000-0000-0000-0000-000000000001", generation)).Want(http.StatusConflict)
			var after pgtype.Timestamptz
			dbfx.QueryRow(t, `SELECT started_at FROM agent_task_queue WHERE id=$1`, id).Scan(&after)
			if !before.Valid || !before.Time.Equal(after.Time) {
				t.Fatalf("replay changed started_at: %v -> %v", before, after)
			}
		})
	}
}

func TestStartClaimReclaimedGeneration(t *testing.T) {
	id, runtimeID, generation := startClaimFixture(t, "dispatched")
	// Reclaim refreshes dispatched_at even if the runtime stays the same.
	newGeneration := generation.Add(time.Microsecond)
	dbfx.Exec(t, `UPDATE agent_task_queue SET dispatched_at=$2 WHERE id=$1`, id, newGeneration)
	testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation)).Want(http.StatusConflict)
	testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, newGeneration)).Want(http.StatusOK)
	testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation)).Want(http.StatusConflict)
}

func TestStartClaimConcurrentReplay(t *testing.T) {
	id, runtimeID, generation := startClaimFixture(t, "dispatched")
	const workers = 12
	results := make(chan *httptest.ResponseRecorder, workers)
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			w := httptest.NewRecorder()
			testHandler.StartTask(w, startClaimRequest(id, runtimeID, generation))
			results <- w
		}()
	}
	close(gate)
	wg.Wait()
	close(results)
	var started string
	for w := range results {
		if w.Code != http.StatusOK {
			t.Fatalf("concurrent start: %d %s", w.Code, w.Body.String())
		}
		var task AgentTaskResponse
		if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		if task.StartedAt == nil {
			t.Fatal("missing started_at")
		}
		if started != "" && started != *task.StartedAt {
			t.Fatal("multiple start timestamps")
		}
		started = *task.StartedAt
	}
}

func TestStartClaimCancellationRace(t *testing.T) {
	for _, replay := range []bool{false, true} {
		t.Run(fmt.Sprintf("replay=%t", replay), func(t *testing.T) {
			id, runtimeID, generation := startClaimFixture(t, "dispatched")
			if replay {
				testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation)).Want(http.StatusOK)
			}
			ctx := context.Background()
			tx, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err := tx.Exec(ctx, `UPDATE agent_task_queue SET status='cancelled' WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
			result := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				w := httptest.NewRecorder()
				testHandler.StartTask(w, startClaimRequest(id, runtimeID, generation))
				result <- w
			}()
			// Observe the actual PostgreSQL row-lock wait before committing cancellation.
			deadline := time.Now().Add(5 * time.Second)
			for {
				var blocked bool
				dbfx.QueryRow(t, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '-- name: LockAgentTaskStartClaim%')`).Scan(&blocked)
				if blocked {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("start did not wait on cancellation lock")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case w := <-result:
				if w.Code != http.StatusConflict {
					t.Fatalf("cancelled start: %d %s", w.Code, w.Body.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("start blocked after cancellation committed")
			}
			testutil.Call(t, testHandler.StartTask, startClaimRequest(id, runtimeID, generation)).Want(http.StatusConflict)
			var status string
			dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, id).Scan(&status)
			if status != "cancelled" {
				t.Fatalf("cancellation overwritten: %s", status)
			}
		})
	}
}

func TestStartClaimInvalidBodiesAndLegacy(t *testing.T) {
	id, runtimeID, generation := startClaimFixture(t, "dispatched")
	for _, body := range []any{
		map[string]string{"runtime_id": runtimeID},
		map[string]string{"dispatched_at": generation.Format(time.RFC3339Nano)},
		map[string]string{"runtime_id": "invalid", "dispatched_at": generation.Format(time.RFC3339Nano)},
		map[string]string{"runtime_id": runtimeID, "dispatched_at": generation.Add(time.Nanosecond).Format(time.RFC3339Nano)},
	} {
		req := withURLParam(newOwnerDaemonRequest("POST", "/start", body), "taskId", id)
		testutil.Call(t, testHandler.StartTask, req).Want(http.StatusBadRequest)
	}
	for _, code := range []int{http.StatusOK, http.StatusBadRequest} {
		req := withURLParam(newOwnerDaemonRequest("POST", "/start", nil), "taskId", id)
		testutil.Call(t, testHandler.StartTask, req).Want(code)
	}
}

func TestStartClaimWirePrecision(t *testing.T) {
	id, runtimeID, generation := startClaimFixture(t, "dispatched")
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(id))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := testHandler.Queries.GetAgentRuntime(context.Background(), parseUUID(runtimeID))
	if err != nil {
		t.Fatal(err)
	}
	req := newOwnerDaemonRequest("POST", "/claim", nil)
	// Exercise non-UTC database timestamp locations even on a UTC CI runner.
	for _, zone := range []*time.Location{time.UTC, time.FixedZone("UTC+08", 8*60*60), time.FixedZone("UTC-07", -7*60*60)} {
		t.Run(zone.String(), func(t *testing.T) {
			claimed := task
			claimed.DispatchedAt.Time = task.DispatchedAt.Time.In(zone)
			resp, _, _, _, _, failure := testHandler.buildClaimedTaskResponse(req, &claimed, runtime, runtimeID, testWorkspaceID)
			if failure != nil {
				t.Fatalf("build claim: %+v", failure)
			}
			want := generation.UTC().Format(time.RFC3339Nano)
			if !resp.StartClaimSupported || resp.DispatchedAt == nil {
				t.Fatalf("missing claim timestamp or capability: supported=%t timestamp=%v", resp.StartClaimSupported, resp.DispatchedAt)
			}
			if *resp.DispatchedAt != want {
				t.Fatalf("claim timestamp = %q, want canonical UTC with microseconds %q", *resp.DispatchedAt, want)
			}
		})
	}
}

func TestBuildClaimedTaskResponse_EnterpriseMCP(t *testing.T) {
	if testHandler == nil {
		t.Skip("PostgreSQL is required")
	}
	ctx := context.Background()

	// Mock platform-opc gateway
	mockPlatform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/integration/v1/mcp/services" && r.URL.Query().Get("tenant") == "32500" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"services": [
					{
						"service_id": "weknora-kb",
						"tools": [
							{
								"name": "kb_search",
								"description": "Search knowledge base",
								"input_schema": {"type": "object", "properties": {"q": {"type": "string"}}}
							}
						]
					}
				]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockPlatform.Close()

	origPlatformClient := testHandler.PlatformClient
	testHandler.PlatformClient = platformclient.NewClient(mockPlatform.URL, "test-token", 5*time.Second)
	defer func() { testHandler.PlatformClient = origPlatformClient }()

	// Create department workspace dept-12500
	var deptWorkspaceID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Dept 12500', 'dept-12500', '', 'DPT')
		RETURNING id
	`).Scan(&deptWorkspaceID)
	if err != nil {
		t.Fatalf("failed to insert dept workspace: %v", err)
	}

	runtimeID := handlerTestRuntimeID(t)
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'dept-test-agent', '', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, deptWorkspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create dept agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	taskID := insertHandlerTestTask(t, agentID)

	taskRow, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}

	runtime, err := testHandler.Queries.GetAgentRuntime(ctx, parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("failed to get runtime: %v", err)
	}

	req := newOwnerDaemonRequest("POST", "/claim", nil)
	resp, _, _, _, _, failure := testHandler.buildClaimedTaskResponse(req, &taskRow, runtime, runtimeID, deptWorkspaceID)
	if failure != nil {
		t.Fatalf("buildClaimedTaskResponse failed: %+v", failure)
	}

	if len(resp.RemoteMCPConnections) != 1 {
		t.Fatalf("expected 1 remote mcp connection, got %d", len(resp.RemoteMCPConnections))
	}
	conn := resp.RemoteMCPConnections[0]
	if conn.ContributionID != remotemcp.EnterpriseContributionPrefix+"weknora-kb" {
		t.Errorf("unexpected ContributionID: %s", conn.ContributionID)
	}
	if conn.ContributionKey != "enterprise-weknora-kb" {
		t.Errorf("unexpected ContributionKey: %s", conn.ContributionKey)
	}
	if conn.Transport != "remote_bridge" {
		t.Errorf("unexpected Transport: %s", conn.Transport)
	}
	if conn.Endpoint != mockPlatform.URL {
		t.Errorf("unexpected Endpoint: %s", conn.Endpoint)
	}
	if len(conn.ApprovedTools) != 1 || conn.ApprovedTools[0].Name != "kb_search" {
		t.Errorf("unexpected ApprovedTools: %+v", conn.ApprovedTools)
	}
	if conn.ApprovedTools[0].SchemaDigest == "" {
		t.Errorf("expected non-empty SchemaDigest")
	}

	var pubCfg struct {
		TenantID  string `json:"tenant_id"`
		ServiceID string `json:"service_id"`
		TaskID    string `json:"task_id"`
	}
	if err := json.Unmarshal(conn.PublicConfig, &pubCfg); err != nil {
		t.Fatalf("unmarshal public config: %v", err)
	}
	if pubCfg.TenantID != "32500" || pubCfg.ServiceID != "weknora-kb" || pubCfg.TaskID != taskID {
		t.Errorf("unexpected public config: %+v", pubCfg)
	}

	// Test remoteMCPDaemonTokenForClaim does not require runtime.DaemonID for enterprise connections
	runtimeWithoutDaemon := runtime
	runtimeWithoutDaemon.DaemonID = pgtype.Text{Valid: false}
	token, daemonTokens, err := remoteMCPDaemonTokenForClaim(resp, runtimeWithoutDaemon)
	if err != nil {
		t.Errorf("remoteMCPDaemonTokenForClaim should not fail for enterprise connections: %v", err)
	}
	if token != "" || len(daemonTokens) != 0 {
		t.Errorf("expected empty token and daemonTokens for pure enterprise connections, got token=%s, count=%d", token, len(daemonTokens))
	}
}
