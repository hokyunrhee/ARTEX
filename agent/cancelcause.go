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
		"The user paused the task via the task control API (POST /api/tasks/{id}/control, action=pause). This Planner/Worker run was actively cancelled; the running intent returns to frontier(open), and after the task resumes it is reclaimed and executed from the start")
	AbortPausedByOrchestrator = cause("paused_by_orchestrator", "Orchestrator agent paused the task",
		"The orchestrator agent called the pause_task tool to pause this task. This Planner/Worker run was actively cancelled; the running intent returns to frontier(open) and runs again after resume")
	AbortTaskDeleted = cause("task_deleted", "Task was deleted",
		"The task is being deleted (DELETE /api/tasks/{id}); the deletion barrier has cancelled this task's running Planner, Worker and main agent, and this run's result will no longer be used")
	AbortPausedOnReload = cause("paused_on_reload", "Backend restored the paused state",
		"On backend startup the task's pause was restored from the state persisted in the database. This run was cancelled; normally no agent is running during the restore phase")
	AbortGoalMet = cause("goal_met", "Planner judged the task goal met",
		"The planner judged the task goal met and set the task to done, then cancelled the still-running Workers; these intents are marked stopped rather than failed")
	AbortSettleDrainTimeout = cause("settle_drain_timeout", "Task timeout wrap-up wait is exhausted",
		"After the task reached timeout it waited for the running Worker to wrap up gracefully, but the 90-second drain grace period was still not enough, so a hard cancel was performed; the intent is marked exhausted, and the facts and assets already written during wrap-up are kept")

	// Per-work context.
	AbortKilledByPlanner = cause("killed_by_planner", "Planner terminated this intent",
		"The planner called kill_work to actively terminate this intent, usually meaning the direction went off track or there was no further value; the intent is marked stopped and is not reclaimed automatically")
	AbortWorkPausedByUser = cause("work_paused_by_user", "User paused this Worker intent",
		"The user paused the running Worker. This call was cancelled and the intent becomes paused; the already-registered intents, facts, findings and activity records are all kept, and after resume it runs again from the start")
	AbortWorkCancelledByUser = cause("work_cancelled_by_user", "User deleted this Worker intent",
		"The user deleted the running Worker. This call was cancelled; after the Worker leaves the write region, the server handles the intent according to the deletion mode the user chose — a soft delete only marks it deleted and keeps all output, while a hard delete cascades to remove this intent and the downstream nodes supported only by it")
	AbortWorkFinished = cause("work_finished", "Worker ended normally, freed context",
		"The Worker ended normally, and the engine freed its context resources in detachWork. This is not a run interruption; if it appears in an interruption message, the cancel and the teardown event raced")
	AbortPausedRaceGuard = cause("paused_race_guard", "Refused a new run while task paused",
		"While the task is paused, the engine refuses to issue a new execution context, to prevent a race between claim and pause from letting a Worker still start; the claimed intent returns to frontier")

	// Main Agent and standalone conversation contexts.
	AbortChatStoppedByUser = cause("chat_stopped_by_user", "User stopped this conversation turn",
		"The user clicked stop, actively aborting this run of the main agent or chat agent. The activity records already produced are kept, and the next message can still be sent")
	AbortChatPausedWithTask = cause("chat_paused_with_task", "Task pause aborted the main agent chat",
		"When the user paused the task, the running main agent chat was cancelled along with it. The activity records already produced are kept; after the task resumes, this message is not replayed automatically")
	AbortChatTurnFinished = cause("chat_turn_finished", "Chat turn ended normally, freed context",
		"This chat turn ended normally, and the server is freeing that turn's context resources. This is not a run interruption; if it appears in an interruption message, the cancel and the teardown event raced")

	// Process-level and per-run hard backstop.
	AbortShutdown = cause("shutdown", "Backend process is shutting down",
		"The backend process received SIGINT or SIGTERM and is restarting, updating or shutting down. All running agents are cancelled; after restart, any leftover running intents are reset to open and run again")
	AbortRunHardTimeout = cause("run_hard_timeout", "A run's hard-timeout backstop fired",
		"A single run exceeded its soft wall-clock budget plus the extra grace period, meaning a model request or some tool did not return for a long time, so the normal turn-boundary wrap-up could not run. Focus on the last tool call that had not returned before the interruption")
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
		return "deadline_exceeded", "Upstream context hit its deadline",
			"Upstream context hit its deadline, but the setter did not attach a named reason via WithTimeoutCause: " + c.Error(), true
	case errors.Is(c, context.Canceled):
		return "canceled_no_cause", "Canceller attached no named reason",
			"The upstream context was cancelled, but the canceller did not attach a named reason via context.WithCancelCause; register a reason in agent/cancelcause.go and wire it into that cancellation point", true
	default:
		return "other", firstLine(c.Error(), 80), c.Error(), true
	}
}
