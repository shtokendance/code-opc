package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
)

// isInternalMikaTask determines whether the task should be dispatched
// to the in-process Mika Goroutine thread instead of spawning an external CLI sub-process.
func (d *Daemon) isInternalMikaTask(task Task) bool {
	if task.Agent != nil {
		if strings.EqualFold(task.Agent.Name, "mika") {
			return true
		}
	}
	if task.ChatSessionID != "" {
		// Chat tasks targeting the default assistant run in-process
		if task.Agent == nil || strings.EqualFold(task.Agent.Name, "mika") {
			return true
		}
	}
	if task.QuickCreatePrompt != "" {
		return true
	}
	return false
}

// runMikaInternalThread executes a task inside a dedicated in-process Goroutine,
// directly invoking the Go SDK and avoiding any external sub-process or CLI invocations.
func (d *Daemon) runMikaInternalThread(ctx context.Context, task Task, taskLog *slog.Logger) (TaskResult, error) {
	taskLog.Info("running mika task in-process", "task_id", task.ID, "chat_session", task.ChatSessionID)

	// Step 1: Start task so server records status=running
	_, err := d.client.StartTask(ctx, task)
	if err != nil {
		return TaskResult{}, fmt.Errorf("start task failed: %w", err)
	}

	_ = d.client.ReportProgress(ctx, task.ID, "Mika 正在处理...", 1, 2)

	// Step 2: Initialize in-process Go SDK client
	token := task.AuthToken
	if token == "" {
		token = d.client.token
	}
	serverURL := d.cfg.ServerBaseURL
	if serverURL == "" {
		serverURL = d.client.baseURL
	}
	sdkClient := cli.NewAPIClient(serverURL, task.WorkspaceID, token)

	// Step 3: Handle Quick-Create prompt
	if task.QuickCreatePrompt != "" {
		return d.handleMikaQuickCreate(ctx, task, sdkClient, taskLog)
	}

	// Step 4: Handle Chat message
	return d.handleMikaChatMessage(ctx, task, sdkClient, taskLog)
}

func (d *Daemon) handleMikaQuickCreate(ctx context.Context, task Task, sdkClient *cli.APIClient, taskLog *slog.Logger) (TaskResult, error) {
	prompt := strings.TrimSpace(task.QuickCreatePrompt)
	lines := strings.Split(prompt, "\n")
	title := strings.TrimSpace(lines[0])
	title = strings.TrimPrefix(title, "创建任务:")
	title = strings.TrimPrefix(title, "创建任务：")
	title = strings.TrimPrefix(title, "新建任务:")
	title = strings.TrimPrefix(title, "新建任务：")
	title = strings.TrimSpace(title)
	if title == "" {
		title = "新任务"
	}

	desc := prompt
	var projID *string
	if task.ProjectID != "" {
		projID = &task.ProjectID
	} else {
		// Attempt to match with existing projects
		projects, err := sdkClient.ListProjects(ctx)
		if err == nil && len(projects) > 0 {
			for _, p := range projects {
				if strings.Contains(prompt, p.Title) {
					pid := p.ID
					projID = &pid
					break
				}
			}
			if projID == nil {
				pid := projects[0].ID
				projID = &pid
			}
		}
	}

	originType := "quick_create"
	originID := task.ID

	var assigneeType, assigneeID *string
	if task.SquadID != "" {
		t := "squad"
		id := task.SquadID
		assigneeType, assigneeID = &t, &id
	} else if task.AgentID != "" {
		t := "agent"
		id := task.AgentID
		assigneeType, assigneeID = &t, &id
	}

	var parentIssueID *string
	if task.ParentIssueID != "" {
		pid := task.ParentIssueID
		parentIssueID = &pid
	}

	var dueDate *string
	if task.QuickCreateDueDate != "" {
		dd := task.QuickCreateDueDate
		dueDate = &dd
	}

	req := cli.CreateIssueRequest{
		Title:         title,
		Description:   &desc,
		Priority:      task.QuickCreatePriority,
		AssigneeType:  assigneeType,
		AssigneeID:    assigneeID,
		ParentIssueID: parentIssueID,
		ProjectID:     projID,
		DueDate:       dueDate,
		AttachmentIDs: task.QuickCreateAttachmentIDs,
		OriginType:    &originType,
		OriginID:      &originID,
	}

	issue, err := sdkClient.CreateIssue(ctx, req)
	if err != nil {
		taskLog.Error("failed to create issue via in-process SDK", "error", err)
		return TaskResult{}, fmt.Errorf("create issue via SDK: %w", err)
	}

	output := fmt.Sprintf("✅ 已为您创建任务：**[%s] %s**", issue.Identifier, issue.Title)
	return TaskResult{
		Status:  "completed",
		Comment: output,
	}, nil
}

func (d *Daemon) handleMikaChatMessage(ctx context.Context, task Task, sdkClient *cli.APIClient, taskLog *slog.Logger) (TaskResult, error) {
	msg := strings.TrimSpace(task.ChatMessage)
	taskLog.Info("processing chat message", "content", msg)

	// Fetch projects and recent issues for genuine context
	projects, _ := sdkClient.ListProjects(ctx)
	issues, _ := sdkClient.ListIssues(ctx, cli.ListIssueOptions{Limit: 10})

	lowerMsg := strings.ToLower(msg)

	// 1. Check if user wants to create an issue
	if strings.Contains(msg, "创建") || strings.Contains(msg, "新建") || strings.Contains(msg, "加个任务") || strings.Contains(msg, "提个issue") || strings.Contains(lowerMsg, "create issue") {
		title := extractIssueTitleFromText(msg)
		desc := msg
		var projID *string
		if len(projects) > 0 {
			for _, p := range projects {
				if strings.Contains(msg, p.Title) {
					pid := p.ID
					projID = &pid
					break
				}
			}
			if projID == nil {
				pid := projects[0].ID
				projID = &pid
			}
		}

		issue, err := sdkClient.CreateIssue(ctx, cli.CreateIssueRequest{
			Title:       title,
			Description: &desc,
			ProjectID:   projID,
		})
		if err == nil {
			reply := fmt.Sprintf("已成功为您创建任务：**[%s] %s**\n\n您可以随时在任务列表中查看详情或进行后续指派。", issue.Identifier, issue.Title)
			return TaskResult{
				Status:  "completed",
				Comment: reply,
			}, nil
		}
	}

	// 2. Check if user is asking about projects
	if strings.Contains(msg, "项目") || strings.Contains(lowerMsg, "project") {
		var sb strings.Builder
		if len(projects) == 0 {
			sb.WriteString("当前工作区暂无项目，您可以随时告诉我帮您创建新项目或任务。\n")
		} else {
			sb.WriteString("当前工作区的项目列表如下：\n\n")
			for i, p := range projects {
				desc := "无描述"
				if p.Description != nil && *p.Description != "" {
					desc = *p.Description
				}
				sb.WriteString(fmt.Sprintf("%d. **%s**（任务数: %d，已完成: %d）\n   - %s\n", i+1, p.Title, p.IssueCount, p.DoneCount, desc))
			}
		}
		return TaskResult{
			Status:  "completed",
			Comment: sb.String(),
		}, nil
	}

	// 3. Check if user is asking about issues / tasks
	if strings.Contains(msg, "任务") || strings.Contains(lowerMsg, "issue") || strings.Contains(msg, "清单") {
		var sb strings.Builder
		if len(issues) == 0 {
			sb.WriteString("当前工作区暂无任务。\n")
		} else {
			sb.WriteString("最近的任务列表：\n\n")
			for i, iss := range issues {
				sb.WriteString(fmt.Sprintf("%d. **[%s] %s** (`%s`)\n", i+1, iss.Identifier, iss.Title, iss.Status))
			}
		}
		return TaskResult{
			Status:  "completed",
			Comment: sb.String(),
		}, nil
	}

	// 4. Default Assistant greeting or consultation
	var reply string
	if strings.Contains(msg, "你好") || strings.Contains(msg, "你是谁") || strings.Contains(lowerMsg, "hello") || strings.Contains(lowerMsg, "hi") {
		reply = "你好！我是你的智能体助手 **Mika**（Chief of Staff）。\n\n我现在作为客户端内部线程常驻运行，随时可以帮你：\n- 快速创建与规划任务（直接说“帮我创建任务: ...”）\n- 查询当前工作区的所有项目与进行中任务\n- 梳理业务需求与架构设计\n\n有什么我可以为你效劳的吗？"
	} else {
		reply = fmt.Sprintf("收到您的需求：“%s”。\n\n我已经同步读取了当前工作区上下文（包含 %d 个项目和 %d 个近期任务）。如果您需要将其转化为具体任务，请告诉我，我可以立即为您创建；或者我们进一步细化具体方案。", msg, len(projects), len(issues))
	}

	return TaskResult{
		Status:  "completed",
		Comment: reply,
	}, nil
}

func extractIssueTitleFromText(text string) string {
	cleaned := text
	prefixes := []string{
		"帮我创建任务:", "帮我创建任务：", "帮我创建任务", "帮我新建任务:", "帮我新建任务：", "帮我新建任务",
		"创建任务:", "创建任务：", "创建任务", "新建任务:", "新建任务：", "新建任务",
		"帮我提一个issue:", "帮我提一个issue：", "加个任务:", "加个任务：",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(cleaned, p) {
			cleaned = strings.TrimPrefix(cleaned, p)
			break
		}
	}
	lines := strings.Split(strings.TrimSpace(cleaned), "\n")
	title := strings.TrimSpace(lines[0])
	if title == "" {
		title = "待办任务"
	}
	if len(title) > 60 {
		title = title[:60] + "..."
	}
	return title
}
