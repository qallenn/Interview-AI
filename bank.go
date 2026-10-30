package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Offline question bank and heuristic scoring, used when Claude is not
// configured or a Claude call fails.

type bankQ struct {
	Title, Text, Tip string
	Keywords         []string // technical terms a good answer is likely to mention
}

var warmUp = bankQ{"Tell me about yourself", "Tell me about yourself.", "Keep it to a minute: your studies, your main project, and the job you want.", nil}

var behavioralBank = []bankQ{
	{"A project you are proud of", "Tell me about a project you are proud of. What was your role?", "Say \"I did\", not only \"we did\", and end with the result.", nil},
	{"Solving a difficult problem", "Tell me about a time you solved a difficult problem.", "Use situation, task, action, result, and give the result in numbers.", nil},
	{"Working with feedback", "Tell me about a time you received critical feedback.", "Be honest about the feedback, then explain what you changed afterwards.", nil},
	{"A missed deadline", "Describe a time you missed a deadline or nearly did. What happened?", "Own the mistake, explain what you did, and what you would do differently.", nil},
	{"Disagreeing with a teammate", "Tell me about a time you disagreed with a teammate. How did you handle it?", "Show you listened, explain how you decided, and how the relationship ended up.", nil},
	{"Learning something fast", "Tell me about a time you had to learn something new very quickly.", "Name what you learned, how you learned it, and what you built with it.", nil},
	{"Why this role", "Why do you want to work as a %s?", "Connect your experience to the job, and be specific about what attracts you.", nil},
}

var technicalBank = map[string][]bankQ{
	"Frontend developer": {
		{"React state and props", "What is the difference between state and props in React?", "Props come from the parent and are read-only; state belongs to the component.", []string{"read-only", "parent", "component", "re-render", "immutable", "setstate", "usestate", "child"}},
		{"Finding a performance problem", "A page in your app is slow. How would you find out what is causing it?", "Name the tool you would use, what you would look for, and how you would measure the fix.", []string{"devtools", "performance", "lighthouse", "profil", "network", "bundle", "lazy", "render", "measure"}},
		{"How the browser renders", "What happens when you type a URL into the browser and press Enter?", "Walk through DNS, the request, the response, parsing HTML and CSS, and rendering.", []string{"dns", "http", "tcp", "html", "css", "dom", "render", "server", "request"}},
		{"Accessibility basics", "How do you make a web page accessible?", "Mention semantic HTML, keyboard use, contrast and screen readers.", []string{"semantic", "aria", "keyboard", "contrast", "screen reader", "alt", "label", "focus"}},
		{"CSS layout", "When would you use Flexbox and when would you use Grid?", "Flexbox is one-dimensional, Grid is two-dimensional; give an example of each.", []string{"one-dimensional", "two-dimensional", "row", "column", "align", "layout", "grid", "flex"}},
	},
	"Backend developer": {
		{"REST API design", "How would you design a REST API for a simple to-do app?", "Name the resources, the HTTP methods, status codes and how you would handle errors.", []string{"get", "post", "put", "delete", "status", "resource", "json", "endpoint"}},
		{"SQL vs NoSQL", "When would you choose a SQL database over a NoSQL one?", "Talk about structure, relations, transactions and scaling.", []string{"relation", "schema", "transaction", "acid", "scal", "join", "document", "consisten"}},
		{"Slow endpoint", "An API endpoint has become slow. How do you investigate?", "Say how you would measure, where you would look first, and how you would confirm the fix.", []string{"log", "profil", "index", "query", "cache", "latency", "monitor", "n+1"}},
		{"Authentication", "How would you store user passwords safely?", "Mention hashing with a slow algorithm, salts, and never storing plain text.", []string{"hash", "salt", "bcrypt", "argon", "pbkdf", "plain", "slow"}},
		{"Concurrency", "What is a race condition and how do you prevent one?", "Give an example and name a fix such as locks or transactions.", []string{"thread", "lock", "mutex", "shared", "transaction", "atomic", "simultaneous"}},
	},
	"Data analyst": {
		{"Cleaning messy data", "You get a dataset with missing values and duplicates. What do you do?", "Explain how you find the problems, how you decide what to fix, and how you check the result.", []string{"missing", "duplicate", "null", "impute", "remove", "check", "outlier", "validate"}},
		{"SQL joins", "Explain the difference between an inner join and a left join.", "Describe which rows each keeps and give a small example.", []string{"match", "rows", "null", "left", "both", "table", "all"}},
		{"Mean vs median", "When is the median a better summary than the mean?", "Mention skewed data and outliers with an example.", []string{"outlier", "skew", "salary", "distribution", "extreme", "middle"}},
		{"Presenting insights", "How would you present a finding to a non-technical manager?", "Lead with the conclusion, use one clear chart, and say what to do next.", []string{"chart", "simple", "recommend", "story", "visual", "conclusion", "action"}},
		{"A/B test", "How would you check whether a new website button increased sign-ups?", "Talk about a control group, sample size and statistical significance.", []string{"control", "sample", "significan", "p-value", "random", "group", "conversion"}},
	},
	"QA engineer": {
		{"Testing a login form", "How would you test a login form?", "Cover positive and negative cases, edge cases and security.", []string{"valid", "invalid", "empty", "edge", "password", "security", "boundary", "error"}},
		{"Writing a bug report", "What makes a good bug report?", "Steps to reproduce, expected vs actual result, environment and severity.", []string{"steps", "reproduce", "expected", "actual", "environment", "severity", "screenshot"}},
		{"Manual vs automated", "When should a test be automated and when should it stay manual?", "Talk about repetition, stability and cost.", []string{"regression", "repeat", "stable", "cost", "exploratory", "time", "maintain"}},
		{"Test levels", "Explain the difference between unit, integration and end-to-end tests.", "Say what each one covers and how many of each you would expect.", []string{"unit", "integration", "end-to-end", "pyramid", "component", "user", "fast"}},
		{"Bug found before release", "You find a serious bug one hour before release. What do you do?", "Explain how you judge the risk, who you tell, and what you recommend.", []string{"severity", "risk", "inform", "team", "decision", "workaround", "priority"}},
	},
	"Project manager": {
		{"Planning a project", "How do you plan a new project from scratch?", "Talk about scope, stakeholders, milestones and risks.", []string{"scope", "stakeholder", "milestone", "risk", "timeline", "goal", "plan"}},
		{"Scope creep", "A client keeps adding new requests. How do you handle it?", "Explain how you assess impact and agree on changes.", []string{"scope", "priorit", "impact", "change request", "timeline", "budget", "agree"}},
		{"Agile vs waterfall", "When would you use Agile instead of Waterfall?", "Compare how requirements and feedback are handled in each.", []string{"iteration", "sprint", "requirement", "feedback", "flexible", "fixed", "change"}},
		{"Late project", "Your project is two weeks behind. What do you do?", "Show how you find the cause, communicate, and adjust the plan.", []string{"cause", "communicat", "stakeholder", "priorit", "scope", "resource", "plan"}},
		{"Measuring success", "How do you know a project was successful?", "Name concrete metrics and how you tracked them.", []string{"metric", "goal", "kpi", "budget", "deadline", "feedback", "measure"}},
	},
	"UX designer": {
		{"Your design process", "Walk me through your design process.", "Go from research to testing, with one real example.", []string{"research", "user", "persona", "wireframe", "prototype", "test", "iterate"}},
		{"Usability testing", "How would you run a usability test?", "Talk about tasks, participants and what you measure.", []string{"task", "participant", "observe", "think aloud", "measure", "success", "users"}},
		{"Handling disagreement", "A developer says your design is too hard to build. What do you do?", "Show how you understand the constraint and find a solution together.", []string{"constraint", "compromise", "priorit", "talk", "alternative", "user", "together"}},
		{"Accessibility in design", "How do you make sure your designs are accessible?", "Mention contrast, text size, focus states and testing with real users.", []string{"contrast", "size", "focus", "screen reader", "wcag", "color", "keyboard"}},
		{"Measuring a design", "How do you know a redesign worked?", "Name metrics and how you compared before and after.", []string{"metric", "conversion", "before", "after", "task", "time", "satisfaction"}},
	},
	"Marketing specialist": {
		{"Launching a campaign", "How would you plan a campaign for a new product?", "Talk about audience, channels, message and how you measure it.", []string{"audience", "channel", "message", "budget", "kpi", "goal", "target"}},
		{"Measuring a campaign", "Which metrics would you use to judge a social media campaign?", "Go beyond likes: reach, engagement rate, clicks and conversions.", []string{"reach", "engagement", "click", "conversion", "ctr", "roi", "cost"}},
		{"Target audience", "How do you define a target audience?", "Mention research, data and personas.", []string{"research", "persona", "data", "survey", "segment", "demograph", "needs"}},
		{"A campaign that failed", "What would you do if a campaign was not performing?", "Explain how you find out why and what you would test next.", []string{"data", "analy", "test", "a/b", "change", "budget", "audience"}},
		{"Content ideas", "How do you come up with content ideas?", "Talk about your audience's questions, trends and past results.", []string{"audience", "trend", "question", "data", "competitor", "calendar", "result"}},
	},
}

var genericTechnical = []bankQ{
	{"A key skill in your field", "Which skill is most important for a %s, and how have you used it?", "Name the skill, give one real example, and say what came out of it.", []string{"example", "project", "result", "used"}},
	{"A tool you know well", "Which tool do you use most in your work or studies, and why?", "Explain what the tool does, how you used it, and its limits.", []string{"tool", "use", "because", "limit", "example"}},
	{"Explaining a concept", "Explain a key concept from your field as if I were new to it.", "Keep it simple and give an everyday example.", []string{"example", "like", "means", "simple"}},
	{"Handling a mistake", "Tell me about a technical mistake you made and how you fixed it.", "Say what went wrong, how you found it, and what you changed.", []string{"found", "fix", "test", "changed", "learned"}},
}

func bankQuestions(iv *Interview, n int) []Turn {
	pick := func(list []bankQ, kind string) []Turn {
		var out []Turn
		for _, i := range rand.Perm(len(list)) {
			q := list[i]
			text, title := q.Text, q.Title
			if strings.Contains(text, "%s") {
				text = fmt.Sprintf(text, strings.ToLower(iv.Role))
			}
			out = append(out, Turn{Text: text, Title: title, Tip: q.Tip, Kind: kind})
		}
		return out
	}
	tech := technicalBank[iv.Role]
	if tech == nil {
		tech = genericTechnical
	}
	techs, behs := pick(tech, "technical"), pick(behavioralBank, "behavioral")

	turns := []Turn{{Text: warmUp.Text, Title: warmUp.Title, Tip: warmUp.Tip, Kind: "behavioral"}}
	for i := 0; len(turns) < n; i++ {
		switch {
		case iv.Type == "Technical":
			turns = append(turns, techs[i%len(techs)])
		case iv.Type == "Behavioral":
			turns = append(turns, behs[i%len(behs)])
		case i%2 == 0:
			turns = append(turns, behs[(i/2)%len(behs)])
		default:
			turns = append(turns, techs[(i/2)%len(techs)])
		}
	}
	for i := range turns {
		turns[i].Main = i + 1
	}
	return turns
}

func keywordsFor(t Turn) []string {
	for _, list := range technicalBank {
		for _, q := range list {
			if q.Text == t.Text {
				return q.Keywords
			}
		}
	}
	for _, q := range genericTechnical {
		if strings.HasPrefix(t.Text, strings.SplitN(q.Text, "%s", 2)[0]) {
			return q.Keywords
		}
	}
	return nil
}

var (
	numberRe = regexp.MustCompile(`\d|percent|twice|half|times faster|doubled|tripled`)
	resultRe = regexp.MustCompile(`\b(result|so that|as a result|which meant|led to|reduced|increased|improved|saved|faster|grew|learned|learnt|outcome)\b`)
	iRe      = regexp.MustCompile(`\b(i|i'm|i've|my)\b`)
	weRe     = regexp.MustCompile(`\b(we|our|we've)\b`)
	exRe     = regexp.MustCompile(`\b(for example|for instance|once|project|when i|at my|in my|first i|i built|i added|i used|i worked|i fixed|i made)\b`)
)

type signals struct {
	words                    int
	numbers, result, own, ex bool
	techHit                  float64 // 0..1, or -1 when not applicable
}

func analyse(t Turn) signals {
	a := strings.ToLower(t.Answer)
	sg := signals{words: len(strings.Fields(a)), numbers: numberRe.MatchString(a), result: resultRe.MatchString(a), ex: exRe.MatchString(a), techHit: -1}
	sg.own = len(iRe.FindAllString(a, -1)) >= len(weRe.FindAllString(a, -1))
	if kw := keywordsFor(t); len(kw) > 0 && t.Kind == "technical" {
		hits := 0
		for _, k := range kw {
			if strings.Contains(a, k) {
				hits++
			}
		}
		sg.techHit = math.Min(1, float64(hits)/3)
	}
	return sg
}

func heuristicEvaluate(iv *Interview, t Turn, allowFollowUp bool) Evaluation {
	sg := analyse(t)
	score := 40.0
	var good []string
	var fix string
	switch {
	case sg.words < 15:
		score -= 15
		fix = "the answer was very short. Give one concrete example and explain it in a few sentences"
	case sg.words <= 40:
		score += 5
	case sg.words <= 250:
		score += 18
		good = append(good, "good length and detail")
	default:
		score += 8
		fix = "the answer was long. Aim for about two minutes and get to the point faster"
	}
	if sg.ex {
		score += 7
		good = append(good, "you used a real example")
	}
	if t.Kind == "behavioral" {
		if sg.own {
			score += 6
		} else if fix == "" {
			fix = `your own role was not clear. Say "I did...", not only "we did..."`
		}
	}
	if t.Title == warmUp.Title {
		// An introduction is not a story, so results and numbers are not expected.
		score += 15
		if len(good) == 0 {
			good = append(good, "a clear introduction")
		}
	} else if sg.result {
		score += 9
		good = append(good, "you explained the outcome")
	} else if fix == "" {
		fix = "the result was missing. End with what changed because of your actions"
	}
	if sg.numbers {
		score += 8
		good = append(good, "you gave numbers")
	} else if fix == "" && sg.result && t.Title != warmUp.Title {
		fix = "add the result in numbers, for example how much time or money it saved"
	}
	if sg.techHit >= 0 {
		score += 20*sg.techHit - 5
		if sg.techHit >= 0.66 {
			good = append(good, "you used the right technical terms")
		} else if fix == "" {
			fix = "the technical explanation was thin. Name the key concepts and give a short example"
		}
	}
	if iv.Difficulty == "Hard" {
		score -= 6
	} else if iv.Difficulty == "Easy" {
		score += 5
	}
	s := clamp(int(math.Round(score)))
	s = max(15, min(95, s))

	reason := "a solid answer"
	if len(good) > 0 {
		reason = strings.Join(good[:min(2, len(good))], " and ")
	}
	reason += "."
	if fix != "" {
		reason += " To improve: " + fix + "."
	}

	ev := Evaluation{Score: s, Reason: reason}
	if allowFollowUp && t.Title != warmUp.Title && (s < 75 || iv.Difficulty == "Hard") {
		ctx := strings.ToLower(strings.TrimSuffix(t.Title, "."))
		var q, title, tip string
		switch {
		case !sg.result || !sg.numbers:
			q, title, tip = "What was the result of that, and how did you know it worked?", "The result", "Give the outcome in numbers if you can."
		case !sg.own && t.Kind == "behavioral":
			q, title, tip = "What exactly was your own part in that?", "Your own role", "Use \"I\" and name two or three things you did yourself."
		case sg.words < 40:
			q, title, tip = "Can you give me a concrete example of that?", "A concrete example", "Pick one real situation and walk through it step by step."
		default:
			q, title, tip = "If you did it again, what would you do differently?", "Looking back", "Show what you learned, not just what went wrong."
		}
		ev.FollowUp = &Turn{Text: q, Title: title, Tip: tip, Kind: t.Kind, FollowUp: true, Context: "your answer about " + ctx}
	}
	return ev
}

func heuristicReport(iv *Interview) Report {
	var n, words, results, nums, own, ex float64
	var techSum, techN float64
	for _, t := range iv.Turns {
		sg := analyse(t)
		n++
		if sg.words >= 40 && sg.words <= 250 {
			words++
		}
		if sg.result {
			results++
		}
		if sg.numbers {
			nums++
		}
		if sg.own {
			own++
		}
		if sg.ex {
			ex++
		}
		if sg.techHit >= 0 {
			techSum += sg.techHit
			techN++
		} else if t.Score != nil && t.Kind == "technical" {
			techSum += float64(*t.Score) / 100
			techN++
		}
	}
	pct := func(x float64) int { return int(math.Round(100 * x / n)) }
	structure := clamp(int(math.Round(40 + 0.3*float64(pct(words)) + 0.3*float64(pct(ex)))))
	impact := clamp(int(math.Round(30 + 0.4*float64(pct(results)) + 0.3*float64(pct(nums)))))
	accuracy := structure
	if techN > 0 {
		accuracy = clamp(int(math.Round(40 + 55*techSum/techN)))
	}

	r := Report{Skills: map[string]int{"structure": structure, "impact": impact, "accuracy": accuracy}}
	add := func(cond bool, yes, no string) {
		if cond {
			r.Strengths = append(r.Strengths, yes)
		} else {
			r.WeakSpots = append(r.WeakSpots, no)
		}
	}
	add(ex/n >= 0.5, "Real examples from your experience", "Few concrete examples")
	add(results/n >= 0.5, "You explain outcomes", "Results are missing or vague")
	add(nums/n >= 0.4, "Numbers that show impact", "Few numbers or measurable results")
	add(own/n >= 0.6, "Clear about your own role", "Your own role is not always clear")
	add(words/n >= 0.6, "Answers of a good length", "Answers too short or too long")
	if techN > 0 {
		add(techSum/techN >= 0.6, "Solid technical vocabulary", "Technical answers need more depth")
	}
	r.Strengths, r.WeakSpots = firstN(r.Strengths, 3, "Calm, complete answers"), firstN(r.WeakSpots, 3, "Keep answers under two minutes")

	for _, w := range r.WeakSpots {
		switch {
		case strings.Contains(w, "Results"), strings.Contains(w, "numbers"):
			r.PracticeNext = append(r.PracticeNext, "End every story with a measurable result")
		case strings.Contains(w, "examples"):
			r.PracticeNext = append(r.PracticeNext, "The STAR method for stories")
		case strings.Contains(w, "role"):
			r.PracticeNext = append(r.PracticeNext, `Say "I did…", not only "we did…"`)
		case strings.Contains(w, "Technical"):
			r.PracticeNext = append(r.PracticeNext, fmt.Sprintf("Core %s concepts out loud", strings.ToLower(iv.Role)))
		case strings.Contains(w, "short or too long"):
			r.PracticeNext = append(r.PracticeNext, "A one-minute \"about me\"")
		}
	}
	var uniq []string
	for _, p := range r.PracticeNext {
		if !slices.Contains(uniq, p) {
			uniq = append(uniq, p)
		}
	}
	r.PracticeNext = firstN(uniq, 3, "Mock interview with harder follow-ups")

	praise := map[string]string{
		"Real examples from your experience": "You back up your answers with real examples from your experience",
		"You explain outcomes":               "You explain what came out of your work",
		"Numbers that show impact":           "You use numbers to show your impact",
		"Clear about your own role":          "You are clear about what you did yourself",
		"Answers of a good length":           "Your answers have a good length and pace",
		"Solid technical vocabulary":         "You use the right technical terms",
	}[r.Strengths[0]]
	if praise == "" {
		praise = "You stayed calm and answered every question"
	}
	next := r.PracticeNext[0]
	r.Summary = fmt.Sprintf("%s. To score higher, practice this next: %s.", praise, strings.ToLower(next[:1])+next[1:])
	return r
}

func firstN(list []string, n int, filler string) []string {
	if len(list) == 0 {
		list = []string{filler}
	}
	return list[:min(n, len(list))]
}

// sampleInterview is the demo report from the mockup, shown to visitors.
func sampleInterview() map[string]any {
	sc := func(n int) *int { return &n }
	turns := []Turn{
		{Text: "Tell me about yourself.", Title: "Tell me about yourself", Main: 1, Score: sc(82), Reason: "clear and short, with your studies, your main project and the job you want. A strong opening."},
		{Text: "Tell me about a project you are proud of.", Title: "A project you are proud of", Main: 2, Score: sc(64), Reason: `good description of the project, but your own role was not clear. Say "I did…", not only "we did…".`},
		{Text: "How did you find out what was making the page slow?", Title: "Finding a performance problem", Main: 3, FollowUp: true, Score: sc(78), Reason: "you named the tool and the cause. The result was missing: how much faster did the page load after lazy loading?"},
		{Text: "What is the difference between state and props in React?", Title: "React state and props", Main: 4, Score: sc(58), Reason: "the idea is close, but you said props can be changed by the component. Props are read-only; state belongs to the component."},
		{Text: "Tell me about a time you received critical feedback.", Title: "Working with feedback", Main: 5, Score: sc(88), Reason: "honest and well structured, and you explained what you changed afterwards. Keep it under two minutes."},
	}
	now := time.Now()
	return map[string]any{
		"id": "sample", "role": "Frontend developer", "level": "Junior", "type": "Mixed", "difficulty": "Medium",
		"status": "finished", "turns": turns, "planned": []any{}, "mainTotal": 5, "sample": true,
		"createdAt": now.Add(-24 * time.Minute), "finishedAt": now, "prevScore": 70,
		"report": Report{
			FinalScore:   74,
			Summary:      "You explain your projects with confidence and give concrete examples. To score higher, finish each answer with the result and keep technical answers shorter.",
			Strengths:    []string{"Clear structure in stories", "Real examples from projects", "Calm pace of speech"},
			WeakSpots:    []string{"Results are missing or vague", "Long pauses in technical answers", `"State" and "props" mixed up`},
			PracticeNext: []string{"The STAR method for stories", "React basics: state vs props", `A one-minute "about me"`},
			Skills:       map[string]int{"structure": 81, "impact": 59, "accuracy": 64},
		},
	}
}
