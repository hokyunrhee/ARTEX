package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Autumn-27/artex/db"
)

// Manual constraint CRUD and injection-scope settings for overview constraint management.
// Like set_constraints, this writes allow/deny entries to task_constraints. Constraints are
// prompt context only: mutations do NOT notify the planner; the next round reads them naturally.
// All mutations use beginTaskOperation/decInflight to avoid task deletion races, matching goal/intent CRUD.

// Injection toggle settings keys, both enabled by default (GetBool second argument = true).
const (
	settingConstraintsInjectPlanner = "constraints_inject_planner"
	settingConstraintsInjectWorker  = "constraints_inject_worker"
)

// constraintInjectPlanner / constraintInjectWorker report whether operation constraints
// enter the corresponding system prompt (default on). Resolvers read each round, applying changes immediately.
func (s *Server) constraintInjectPlanner() bool {
	return s.m.pg.GetBool(settingConstraintsInjectPlanner, true)
}

func (s *Server) constraintInjectWorker() bool {
	return s.m.pg.GetBool(settingConstraintsInjectWorker, true)
}

// listConstraints returns all task operation constraints, allow before deny.
func (s *Server) listConstraints(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	rows, err := t.Store.ListConstraints()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"constraints": constraintDTOs(rows)})
}

// addConstraint manually adds an allow/deny constraint without notifying the planner.
func (s *Server) addConstraint(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, 409, "Task is being deleted; cannot add constraints")
		return
	}
	defer s.engine.decInflight(t.ID)

	var body struct {
		Text string `json:"text"`
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeErr(w, 400, "Constraint text cannot be empty")
		return
	}
	kind := normalizeConstraintKind(body.Kind)
	if kind == "" {
		writeErr(w, 400, "kind must be allow or deny")
		return
	}
	id, err := t.Store.AddConstraint(kind, text, "human")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, ConstraintDTO{ID: strconv.FormatInt(id, 10), Kind: kind, Text: text, Origin: "human"})
}

// editConstraint manually edits kind/text without notifying the planner.
func (s *Server) editConstraint(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, 409, "Task is being deleted; cannot edit constraints")
		return
	}
	defer s.engine.decInflight(t.ID)

	cid, err := strconv.ParseInt(r.PathValue("cid"), 10, 64)
	if err != nil || cid <= 0 {
		writeErr(w, 400, "bad constraint id")
		return
	}
	var body struct {
		Text string `json:"text"`
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeErr(w, 400, "Constraint text cannot be empty")
		return
	}
	kind := normalizeConstraintKind(body.Kind)
	if kind == "" {
		writeErr(w, 400, "kind must be allow or deny")
		return
	}
	if err := t.Store.UpdateConstraint(cid, kind, text); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, ConstraintDTO{ID: strconv.FormatInt(cid, 10), Kind: kind, Text: text})
}

// deleteConstraint manually removes a constraint without notifying the planner.
func (s *Server) deleteConstraint(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, 409, "Task is being deleted; cannot delete constraints")
		return
	}
	defer s.engine.decInflight(t.ID)

	cid, err := strconv.ParseInt(r.PathValue("cid"), 10, 64)
	if err != nil || cid <= 0 {
		writeErr(w, 400, "bad constraint id")
		return
	}
	if err := t.Store.DeleteConstraint(cid); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// normalizeConstraintKind lowercases + validates the kind; "" on invalid.
func normalizeConstraintKind(k string) string {
	k = strings.TrimSpace(strings.ToLower(k))
	if k == "allow" || k == "deny" {
		return k
	}
	return ""
}

// constraintDTOs converts db rows to the frontend shape.
func constraintDTOs(in []db.Constraint) []ConstraintDTO {
	out := make([]ConstraintDTO, 0, len(in))
	for _, c := range in {
		out = append(out, ConstraintDTO{
			ID:     strconv.FormatInt(c.ID, 10),
			Kind:   c.Kind,
			Text:   c.Text,
			Origin: c.Origin,
			TS:     rfc3339(c.CreatedAt),
		})
	}
	return out
}
