package main

import (
	"context"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"
)

var (
	roles        = []string{"Frontend developer", "Backend developer", "Data analyst", "QA engineer", "Project manager", "UX designer", "Marketing specialist"}
	levels       = []string{"Intern", "Junior", "Middle"}
	types        = []string{"Technical", "Behavioral", "Mixed"}
	difficulties = []string{"Easy", "Medium", "Hard"}
)

const questionsPerInterview = 5

// interviewView is what the client sees: upcoming questions are reduced to
// their titles so the next question stays a surprise.
func (s *server) view(iv *Interview) map[string]any {
	planned := make([]map[string]string, len(iv.Planned))
	for i, p := range iv.Planned {
		planned[i] = map[string]string{"title": p.Title}
	}
	out := map[string]any{
		"id": iv.ID, "role": iv.Role, "level": iv.Level, "type": iv.Type, "difficulty": iv.Difficulty,
		"status": iv.Status, "turns": iv.Turns, "planned": planned, "mainTotal": iv.MainTotal,
		"createdAt": iv.CreatedAt, "finishedAt": iv.FinishedAt, "report": iv.Report,
	}
	// Score of the previous finished interview, for "+4 since your last interview".
	for _, other := range s.store.Interviews(iv.UserID) {
		if other.ID != iv.ID && other.Report != nil && other.CreatedAt.Before(iv.CreatedAt) {
			out["prevScore"] = other.Report.FinalScore
			break
		}
	}
	return out
}

func (s *server) listInterviews(w http.ResponseWriter, r *http.Request, u *User) {
	list := []map[string]any{}
	for _, iv := range s.store.Interviews(u.ID) {
		item := map[string]any{
			"id": iv.ID, "role": iv.Role, "level": iv.Level, "type": iv.Type, "difficulty": iv.Difficulty,
			"status": iv.Status, "createdAt": iv.CreatedAt, "finishedAt": iv.FinishedAt,
			"questions": countAnswered(iv), "answers": countAnswered(iv),
		}
		if iv.Report != nil {
			item["score"] = iv.Report.FinalScore
			item["skills"] = iv.Report.Skills
		}
		list = append(list, item)
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *server) deleteInterviews(w http.ResponseWriter, r *http.Request, u *User) {
	s.store.DeleteInterviews(u.ID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func countAnswered(iv *Interview) int {
	n := 0
	for _, t := range iv.Turns {
		if t.Score != nil {
			n++
		}
	}
	return n
}

func (s *server) createInterview(w http.ResponseWriter, r *http.Request, u *User) {
	var in struct{ Role, Level, Type, Difficulty string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	in.Role = strings.TrimSpace(in.Role)
	if in.Role == "" || len(in.Role) > 80 {
		writeErr(w, http.StatusBadRequest, "please choose a profession")
		return
	}
	if !slices.Contains(levels, in.Level) || !slices.Contains(types, in.Type) || !slices.Contains(difficulties, in.Difficulty) {
		writeErr(w, http.StatusBadRequest, "please choose level, type and difficulty")
		return
	}
	iv := &Interview{
		ID: newID(8), UserID: u.ID, Role: in.Role, Level: in.Level, Type: in.Type,
		Difficulty: in.Difficulty, Status: "active", CreatedAt: time.Now(), MainTotal: questionsPerInterview,
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	qs := s.ai.Questions(ctx, iv, questionsPerInterview)
	iv.Turns, iv.Planned = qs[:1], qs[1:]
	if err := s.store.SaveInterview(iv); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not save interview")
		return
	}
	writeJSON(w, http.StatusCreated, s.view(iv))
}

func (s *server) getInterview(w http.ResponseWriter, r *http.Request, u *User) {
	iv, err := s.store.Interview(u.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "interview not found")
		return
	}
	writeJSON(w, http.StatusOK, s.view(iv))
}

// followUpAllowed decides whether the current answer may get a follow-up,
// based on difficulty: easy never, medium up to two per interview, hard one
// per main question.
func followUpAllowed(iv *Interview) bool {
	cur := iv.Turns[len(iv.Turns)-1]
	if cur.FollowUp {
		return false
	}
	n := 0
	for _, t := range iv.Turns {
		if t.FollowUp {
			n++
		}
	}
	switch iv.Difficulty {
	case "Medium":
		return n < 2
	case "Hard":
		return true
	}
	return false
}

func (s *server) answer(w http.ResponseWriter, r *http.Request, u *User) {
	var in struct {
		Text    string
		Seconds int
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	iv, err := s.store.Interview(u.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "interview not found")
		return
	}
	if iv.Status != "active" {
		writeErr(w, http.StatusConflict, "this interview has already finished")
		return
	}
	idx := len(iv.Turns) - 1
	cur := &iv.Turns[idx]
	cur.Answer = strings.TrimSpace(in.Text)
	if len(cur.Answer) > 8000 {
		cur.Answer = cur.Answer[:8000]
	}
	cur.Seconds = max(0, in.Seconds)

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	ev := s.ai.Evaluate(ctx, iv, idx, followUpAllowed(iv))
	cur.Score, cur.Reason = &ev.Score, ev.Reason

	switch {
	case ev.FollowUp != nil:
		ev.FollowUp.Main = cur.Main
		iv.Turns = append(iv.Turns, *ev.FollowUp)
	case len(iv.Planned) > 0:
		iv.Turns = append(iv.Turns, iv.Planned[0])
		iv.Planned = iv.Planned[1:]
	default:
		s.complete(ctx, iv)
	}
	if err := s.store.SaveInterview(iv); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not save answer")
		return
	}
	writeJSON(w, http.StatusOK, s.view(iv))
}

// finish ends an interview early ("End interview"). Unanswered questions are
// dropped; an interview with no answers at all is discarded.
func (s *server) finish(w http.ResponseWriter, r *http.Request, u *User) {
	iv, err := s.store.Interview(u.ID, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "interview not found")
		return
	}
	if iv.Status == "active" {
		var answered []Turn
		for _, t := range iv.Turns {
			if t.Score != nil {
				answered = append(answered, t)
			}
		}
		if len(answered) == 0 {
			s.store.mu.Lock()
			delete(s.store.d.Interviews, iv.ID)
			s.store.flush()
			s.store.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
			return
		}
		iv.Turns, iv.Planned = answered, nil
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		s.complete(ctx, iv)
		s.store.SaveInterview(iv)
	}
	writeJSON(w, http.StatusOK, s.view(iv))
}

func (s *server) complete(ctx context.Context, iv *Interview) {
	sum := 0
	for _, t := range iv.Turns {
		sum += *t.Score
	}
	final := int(math.Round(float64(sum) / float64(len(iv.Turns))))
	rep := s.ai.Report(ctx, iv)
	rep.FinalScore = final
	now := time.Now()
	iv.Status, iv.FinishedAt, iv.Report = "finished", &now, &rep
}

func (s *server) sampleReport(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, sampleInterview())
}
