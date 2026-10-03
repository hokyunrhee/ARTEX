package agent

import (
	"context"
	"errors"
	"fmt"
)

// AbortCause names why an agent run's context was cancelled. Every cancellation
// site should attach one so the activity trace can report the real initiator.
type AbortCause struct {
	Code  string
	Short string
	Text  string
}

func (c *AbortCause) Error() string { return c.Text }

func cause(code, short, text string) *AbortCause {
	return &AbortCause{Code: code, Short: short, Text: text}
}

// Causef builds a cause that includes runtime-specific detail.
func Causef(code, short, format string, args ...any) *AbortCause {
	return &AbortCause{Code: code, Short: short, Text: fmt.Sprintf(format, args...)}
}

var (
	// Task-level execution context.
	AbortPausedByUser = cause("paused_by_user", "User paused the task",
		"The user paused the task through POST /api/tasks/{id}/control with action=pause. This planner/worker run was explicitly canceled; running intents return to frontier(open) and are claimed again and executed from the start after resuming")
	AbortPausedByOrchestrator = cause("paused_by_orchestrator", "Orchestrator paused the task",
		"The orchestration agent paused this task with pause_task. This planner/worker run was explicitly canceled; running intents return to frontier(open) and are executed again after resuming")
	AbortTaskDeleted = cause("task_deleted", "Task deleted",
		"The task is being deleted through DELETE /api/tasks/{id}. The deletion barrier canceled its running planner, workers, and main agent; this run's results will no longer be used")
	AbortPausedOnReload = cause("paused_on_reload", "Backend restored the paused state",
		"On startup, the backend restored the task's paused state from the database. This run was canceled; normally no agents are running during restoration")
	AbortGoalMet = cause("goal_met", "Planner confirmed the goal was met",
		"The planner determined that the task goal was achieved, set the task to done, then canceled running workers. These intents are marked stopped rather than failed")
	AbortSettleDrainTimeout = cause("settle_drain_timeout", "Task wrap-up wait expired",
		"After the task timed out, the system waited for running workers to wrap up gracefully, but the 90-second drain grace period expired, triggering hard cancellation. Intents are marked exhausted; facts and assets already written during wrap-up are retained")

	// Per-work context.
	AbortKilledByPlanner = cause("killed_by_planner", "Planner stopped this intent",
		"The planner explicitly stopped this intent with kill_work, usually because it drifted off course or no longer merits continuation. The intent is marked stopped and will not be claimed again automatically")
	AbortWorkPausedByUser = cause("work_paused_by_user", "User paused this worker intent",
		"The user paused the running worker. This call was canceled and the intent becomes paused; all recorded intents, facts, findings, and activities are retained, and execution restarts from the beginning after resuming")
	AbortWorkCancelledByUser = cause("work_cancelled_by_user", "User deleted this worker intent",
		"The user deleted the running worker. This call was canceled; after the worker leaves the write section, the server applies the selected deletion mode: soft deletion only marks the intent deleted and retains all outputs, while hard deletion cascades to the intent and downstream nodes supported solely by it")
	AbortWorkFinished = cause("work_finished", "Worker finished and released context",
		"The worker finished normally and the engine released its context resources in detachWork. This is not an interruption; if shown as one, cancellation and completion events raced")
	AbortPausedRaceGuard = cause("paused_race_guard", "New run blocked while task is paused",
		"The engine refused to issue a new execution context while the task was paused, preventing a claim/pause race from starting a worker. The claimed intent returns to frontier")

	// Main Agent and standalone conversation contexts.
	AbortChatStoppedByUser = cause("chat_stopped_by_user", "User stopped this conversation turn",
		"The user clicked Stop, explicitly ending this main-agent or conversation-agent run. Existing activity records are retained, and another message can be sent")
	AbortChatPausedWithTask = cause("chat_paused_with_task", "Task pause stopped the main-agent chat",
		"Pausing the task also canceled the running main-agent conversation. Existing activity records are retained; resuming the task does not automatically replay this turn's message")
	AbortChatTurnFinished = cause("chat_turn_finished", "Turn finished and released context",
		"This conversation turn finished normally and the server is releasing its context resources. This is not an interruption; if shown as one, cancellation and completion events raced")

	// Process-level and per-run hard backstop.
	AbortShutdown = cause("shutdown", "Backend is shutting down",
		"The backend received SIGINT or SIGTERM and is restarting, updating, or shutting down. All running agents are canceled; after restart, remaining running intents reset to open and execute again")
	AbortRunHardTimeout = cause("run_hard_timeout", "Run hard-timeout fallback triggered",
		"The run exceeded its soft wall-clock budget and extra grace period, indicating that a model request or tool has not returned for a long time and normal turn-boundary wrap-up could not run. Check the last tool call that had not returned before interruption")
)

// AbortReason resolves the named cause attached to a cancelled run context.
func AbortReason(ctx context.Context) (code, short, text string, ok bool) {
	c := context.Cause(ctx)
	if c == nil {
		return "", "", "", false
	}
	var ac *AbortCause
	if errors.As(c, &ac) {
		return ac.Code, ac.Short, ac.Text, true
	}
	switch {
	case errors.Is(c, context.DeadlineExceeded):
		return "deadline_exceeded", "Upstream context deadline reached",
			"The upstream context reached its deadline, but its creator did not attach a named reason through WithTimeoutCause: " + c.Error(), true
	case errors.Is(c, context.Canceled):
		return "canceled_no_cause", "Cancellation has no named reason",
			"The upstream context was canceled without a named reason from context.WithCancelCause. Register a reason in agent/cancelcause.go and wire it into this cancellation point", true
	default:
		return "other", firstLine(c.Error(), 80), c.Error(), true
	}
}
