package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Evaluator writes questions, scores answers and builds reports. With an
// ANTHROPIC_API_KEY it uses Claude; otherwise (or if a call fails) it falls
// back to the built-in question bank and heuristic scoring in bank.go.
type Evaluator struct {
	client *anthropic.Client
	model  string
}

func NewEvaluator() *Evaluator {
	e := &Evaluator{model: envOr("CLAUDE_MODEL", "claude-opus-5-5")}
	if os.Getenv("ANTHROPIC_API_KEY") != "" && os.Getenv("OFFLINE") == "" {
		c := anthropic.NewClient()
		e.client = &c
	}
	return e
}

func (e *Evaluator) Enabled() bool { return e.client != nil }

const systemPrompt = `You are InterviewAI, a friendly but honest job interviewer and coach for students, graduates and career changers practising their first real interviews.
Write in plain, simple English. Be concrete and specific to what the candidate actually said. Never invent facts about the candidate.
Always reply with a single JSON object and nothing else - no markdown fences, no commentary.`

// askJSON sends one prompt and decodes the JSON object Claude replies with.
func (e *Evaluator) askJSON(ctx context.Context, prompt string, out any) error {
	resp, err := e.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:        anthropic.Model(e.model),
		MaxTokens:    16000,
		System:       []anthropic.TextBlockParam{{Text: systemPrompt}},
		OutputConfig: anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffortLow},
		Messages:     []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
	})
	if err != nil {
		return err
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return errors.New("model declined the request")
	}
	var text strings.Builder
	for _, b := range resp.Content {
		if t, ok := b.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	s := text.String()
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i < 0 || j < i {
		return fmt.Errorf("no JSON in reply: %.200s", s)
	}
	return json.Unmarshal([]byte(s[i:j+1]), out)
}

func describe(iv *Interview) string {
	return fmt.Sprintf("Role: %s. Level: %s. Interview type: %s. Difficulty: %s.", iv.Role, iv.Level, iv.Type, iv.Difficulty)
}

func transcript(iv *Interview) string {
	var b strings.Builder
	for i, t := range iv.Turns {
		fu := ""
		if t.FollowUp {
			fu = " (follow-up)"
		}
		fmt.Fprintf(&b, "Q%d%s: %s\nAnswer: %s\n", i+1, fu, t.Text, orNone(t.Answer))
		if t.Score != nil {
			fmt.Fprintf(&b, "Score: %d. Reason: %s\n", *t.Score, t.Reason)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no answer)"
	}
	return s
}

// ---------- questions ----------

func (e *Evaluator) Questions(ctx context.Context, iv *Interview, n int) []Turn {
	if e.Enabled() {
		var out struct {
			Questions []struct{ Text, Title, Kind, Tip string } `json:"questions"`
		}
		prompt := fmt.Sprintf(`Write %d interview questions for this candidate. %s
Order them like a real interview: start with a warm-up ("tell me about yourself" style), then go deeper.
For "Mixed", alternate technical and behavioral. Match depth to the level; harder difficulty means more demanding questions.
Each question must be one or two spoken sentences, natural to say out loud.
Return: {"questions":[{"text":"...","title":"short 2-5 word label","kind":"technical|behavioral","tip":"one sentence hint on what a strong answer includes"}]}`, n, describe(iv))
		err := e.askJSON(ctx, prompt, &out)
		if err == nil && len(out.Questions) >= n {
			turns := make([]Turn, n)
			for i, q := range out.Questions[:n] {
				turns[i] = Turn{Text: q.Text, Title: q.Title, Kind: normKind(q.Kind), Tip: q.Tip, Main: i + 1}
			}
			return turns
		}
		log.Printf("claude questions failed, using question bank: %v", err)
	}
	return bankQuestions(iv, n)
}

func normKind(k string) string {
	if strings.EqualFold(k, "technical") {
		return "technical"
	}
	return "behavioral"
}

// ---------- answer evaluation ----------

type Evaluation struct {
	Score    int
	Reason   string
	FollowUp *Turn
}

func (e *Evaluator) Evaluate(ctx context.Context, iv *Interview, idx int, allowFollowUp bool) Evaluation {
	t := iv.Turns[idx]
	if strings.TrimSpace(t.Answer) == "" {
		return Evaluation{Score: 0, Reason: "no answer was given. Even a short, honest answer scores better than silence."}
	}
	if e.Enabled() {
		var out struct {
			Score    int    `json:"score"`
			Reason   string `json:"reason"`
			FollowUp *struct {
				Text, Title, Context, Tip string
			} `json:"followUp"`
		}
		fu := `Do not ask a follow-up: set "followUp" to null.`
		if allowFollowUp {
			strict := "Ask a follow-up only if something important is missing or unclear (for example the result, the candidate's own role, or a shaky technical point); otherwise set it to null."
			if iv.Difficulty == "Hard" {
				strict = "Ask a probing follow-up that digs deeper into the weakest or most interesting part of the answer, unless the answer is already excellent (score 90+)."
			}
			fu = strict
		}
		strictness := "Score fairly for the level."
		if iv.Difficulty == "Hard" {
			strictness = "Score strictly: expect precise, complete answers with results."
		} else if iv.Difficulty == "Easy" {
			strictness = "Score generously but honestly; this is an easy practice session."
		}
		prompt := fmt.Sprintf(`%s
Interview so far:
%s
Score the LAST answer from 0 to 100. %s Consider: structure (situation, task, action, result for stories), concrete examples, measurable results, technical accuracy, and clarity. The answer is a speech-to-text transcript, so ignore filler words and small transcription errors.
Write "reason" as one or two short sentences starting in lowercase, addressed to the candidate as "you": say what was good and the single most useful fix (e.g. "you named the tool and the cause. The result was missing: how much faster did the page load?").
%s
Return: {"score":0,"reason":"...","followUp":null or {"text":"the spoken follow-up question","title":"short 2-5 word label","context":"short phrase naming what it follows up on, e.g. the university project you rebuilt in React","tip":"one sentence hint"}}`,
			describe(iv), transcript(iv), strictness, fu)
		err := e.askJSON(ctx, prompt, &out)
		if err == nil {
			ev := Evaluation{Score: clamp(out.Score), Reason: out.Reason}
			if allowFollowUp && out.FollowUp != nil && strings.TrimSpace(out.FollowUp.Text) != "" {
				ev.FollowUp = &Turn{Text: out.FollowUp.Text, Title: out.FollowUp.Title, Context: out.FollowUp.Context,
					Tip: out.FollowUp.Tip, Kind: t.Kind, FollowUp: true}
			}
			return ev
		}
		log.Printf("claude evaluation failed, using heuristics: %v", err)
	}
	return heuristicEvaluate(iv, t, allowFollowUp)
}

func clamp(n int) int { return max(0, min(100, n)) }

// ---------- final report ----------

func (e *Evaluator) Report(ctx context.Context, iv *Interview) Report {
	if e.Enabled() {
		var out Report
		prompt := fmt.Sprintf(`%s
Full interview:
%s
Write the final feedback report. Return:
{"summary":"2 sentences: what the candidate does well, then the one change that would raise their score most",
"strengths":["3 short phrases"],"weakSpots":["3 short phrases"],"practiceNext":["3 short, concrete practice topics"],
"skills":{"structure":0-100,"impact":0-100,"accuracy":0-100}}
"structure" = how well answers are organised; "impact" = whether results and outcomes are stated; "accuracy" = technical/factual correctness.`,
			describe(iv), transcript(iv))
		err := e.askJSON(ctx, prompt, &out)
		if err == nil && out.Summary != "" {
			for k, v := range out.Skills {
				out.Skills[k] = clamp(v)
			}
			return out
		}
		log.Printf("claude report failed, using heuristics: %v", err)
	}
	return heuristicReport(iv)
}
