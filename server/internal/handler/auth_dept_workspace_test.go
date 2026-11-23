package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestGetMe_AutoProvisionsDepartmentWorkspaceAndRole(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	email := fmt.Sprintf("dept-user-%d@example.com", time.Now().UnixNano())
	userID := dbfx.User(t, "Dept User", email)
	user, err := testHandler.Queries.GetUser(context.Background(), parseUUID(userID))
	if err != nil {
		t.Fatalf("failed to load user: %v", err)
	}

	expectedSlug := "dept-1250300000000000000"
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, expectedSlug)
	})

	// Case 1: 普通员工登录，创建部门工作区并设为 member，且自动 onboarded
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("X-User-ID", uuidToString(user.ID))
	req.Header.Set("X-User-Email", user.Email)
	req.Header.Set("X-User-Dept-Code", "1250300000000000000")
	req.Header.Set("X-User-Dept-Name", "BDE平台开发部")
	req.Header.Set("X-User-Is-Dept-Manager", "false")

	w := httptest.NewRecorder()
	testHandler.GetMe(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	ws, err := testHandler.Queries.GetWorkspaceBySlug(context.Background(), expectedSlug)
	if err != nil {
		t.Fatalf("expected workspace %s to be created, got err: %v", expectedSlug, err)
	}
	if ws.Name != "BDE平台开发部" {
		t.Errorf("expected workspace name 'BDE平台开发部', got %q", ws.Name)
	}

	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      user.ID,
		WorkspaceID: ws.ID,
	})
	if err != nil {
		t.Fatalf("expected member to exist: %v", err)
	}
	if member.Role != "member" {
		t.Errorf("expected role 'member', got %q", member.Role)
	}

	// 验证已自动标记 onboarded_at
	reloadedUser, err := testHandler.Queries.GetUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if !reloadedUser.OnboardedAt.Valid {
		t.Errorf("expected user to be onboarded (onboarded_at valid)")
	}

	// Case 2: 主管登录，自动晋升为 admin
	req2 := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req2.Header.Set("X-User-ID", uuidToString(user.ID))
	req2.Header.Set("X-User-Email", user.Email)
	req2.Header.Set("X-User-Dept-Code", "1250300000000000000")
	req2.Header.Set("X-User-Dept-Name", "BDE平台开发部")
	req2.Header.Set("X-User-Is-Dept-Manager", "true")

	w2 := httptest.NewRecorder()
	testHandler.GetMe(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w2.Code, w2.Body.String())
	}

	member2, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      user.ID,
		WorkspaceID: ws.ID,
	})
	if err != nil {
		t.Fatalf("expected member to exist: %v", err)
	}
	if member2.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", member2.Role)
	}
}
