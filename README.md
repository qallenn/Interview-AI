# InterviewAI

Practice job interviews out loud with an AI interviewer. A Go backend with a vanilla JS frontend, built from `InterviewAI_Website_Mockup.pdf`.

## Run

```bash
go run .
```

Then open http://localhost:8080.

Without an API key the app works offline: questions come from a built-in question bank, and answers are scored with simple rules. To use Claude for questions, follow-ups, scoring and reports, set a key before starting:

```powershell
$env:ANTHROPIC_API_KEY = "sk-ant-..."
go run .
```

### Run in GoLand

1. **File → Open…**, then pick the `interviewai` folder (the one that holds `go.mod`).
2. If GoLand asks, set the Go SDK (Settings → Go → GOROOT, Go 1.26).
3. Open `main.go` and click the green ▶ next to `func main()`, or set up **Run → Edit Configurations → + → Go Build**:
   - Run kind: **Directory**, Directory: the `interviewai` folder
   - Working directory: the `interviewai` folder (the database is saved to `data/db.json` here)
   - Environment: `ANTHROPIC_API_KEY=...` (optional)
4. Open http://localhost:8080.

The `web/` folder is embedded into the binary, so **restart the app after you edit HTML, CSS or JS**.

## Deploy to Render

GitHub Pages can only host static files, so it cannot run this Go server. Use [Render](https://render.com) instead:

1. Sign in to Render with GitHub.
2. Click **New → Blueprint** and pick this repository. Render reads `render.yaml`.
3. Enter an `ANTHROPIC_API_KEY` if you want Claude scoring, or leave it empty for offline scoring.
4. Click **Apply**. After the build, the site is live at `https://interviewai-xxxx.onrender.com`.

Every push to `master` redeploys automatically.

On the free plan the app sleeps after 15 minutes without visitors (the next visit takes about a minute to wake it), and **files are wiped on every restart or deploy**, so accounts and interviews in `data/db.json` do not last. To keep one shared login working anyway, set `DEMO_EMAIL` and `DEMO_PASSWORD` in the service's **Environment** tab. For lasting data, use a paid plan with a [persistent disk](https://render.com/docs/disks) mounted at `/var/data` and set `DATA_FILE=/var/data/db.json`.

## Environment variables

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `8080` | Port to listen on (set by Render) |
| `ADDR` | `:$PORT` | Full listen address; overrides `PORT` |
| `DATA_FILE` | `data/db.json` | JSON database file |
| `ANTHROPIC_API_KEY` | — | Turns on Claude scoring |
| `CLAUDE_MODEL` | `claude-opus-5-5` | Claude model to use |
| `OFFLINE` | — | Set to anything to ignore the API key |
| `DEMO_EMAIL`, `DEMO_PASSWORD` | — | A login created (or its password reset) on every start, so a shared demo account survives restarts |
| `DEMO_NAME` | `Demo Student` | Name of the demo account |

## Layout

| File | What it does |
|---|---|
| `main.go` | HTTP server, routes, sign-up/log-in (PBKDF2 hashes, session cookie) |
| `interview.go` | Interview flow: create, answer, follow-ups, finish, history |
| `ai.go` | Claude calls: questions, scoring with reasons, final report |
| `bank.go` | Offline question bank, rule-based scoring, sample report |
| `store.go` | Small JSON-file database |
| `web/` | Single-page frontend: landing, auth, setup, voice interview, report, progress, settings |

## API

| Method | Path | |
|---|---|---|
| POST | `/api/auth/register`, `/api/auth/login`, `/api/auth/logout` | Accounts |
| GET / PATCH / DELETE | `/api/me` | Current user |
| GET / POST / DELETE | `/api/interviews` | List, create, delete history |
| GET | `/api/interviews/{id}` | One interview |
| POST | `/api/interviews/{id}/answer` | Submit `{text, seconds}`; returns the next question or the report |
| POST | `/api/interviews/{id}/finish` | End early |
| GET | `/api/sample-report` | Demo report for visitors |

Voice uses the browser's Web Speech API: speech synthesis reads the questions, and speech recognition writes your answer down (Chrome, Edge, Safari). Only text is sent to the server, never audio. In other browsers you type your answers.
