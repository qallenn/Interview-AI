"use strict";

// ---------- tiny helpers ----------
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
const app = $("#app");
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
let me = null;
let cleanups = [];
const onLeave = (fn) => cleanups.push(fn);

async function api(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
    credentials: "same-origin",
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(data.error || "Something went wrong");
    err.status = res.status;
    throw err;
  }
  return data;
}

function toast(msg) {
  const t = $("#toast");
  t.textContent = msg;
  t.classList.add("show");
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => t.classList.remove("show"), 3200);
}

function go(path, replace = false) {
  if (replace) history.replaceState(null, "", path);
  else history.pushState(null, "", path);
  route();
}

document.addEventListener("click", (e) => {
  const a = e.target.closest("a[href]");
  if (!a || a.target || e.metaKey || e.ctrlKey || e.shiftKey) return;
  const url = new URL(a.href, location.href);
  if (url.origin !== location.origin || url.pathname.startsWith("/api/")) return;
  if (url.hash && url.pathname === location.pathname) return; // in-page anchor
  e.preventDefault();
  go(url.pathname + url.search + url.hash);
});
window.addEventListener("popstate", route);

const initials = (name) => name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0].toUpperCase()).join("");
const firstName = (name) => name.split(/\s+/)[0];
const scoreClass = (s) => (s >= 70 ? "" : s >= 60 ? "mid" : "low");
const badgeClass = (s) => (s >= 70 ? "badge-green" : s >= 60 ? "badge-amber" : "badge-red");

function dateLabel(iso) {
  const d = new Date(iso);
  if (d.toDateString() === new Date().toDateString()) return "Today";
  return d.toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}
const mmss = (sec) => `${String(Math.floor(sec / 60)).padStart(2, "0")}:${String(Math.floor(sec % 60)).padStart(2, "0")}`;
const startInterviewHref = () => (me ? "/new" : "/signup");

// ---------- layout pieces ----------
function logo(href = "/") {
  return `<a class="logo" href="${href}"><span class="logo-dot"></span>InterviewAI</a>`;
}

function publicHeader() {
  return `<header class="header"><div class="container">
    ${logo()}
    <nav class="nav">
      <a href="/#how">How it works</a><a href="/#who">Who it’s for</a><a href="/sample">Sample report</a>
    </nav>
    <div class="header-right">${
      me
        ? `<a class="btn btn-dark btn-sm" href="/new">Open app</a>`
        : `<a class="btn btn-outline btn-sm" href="/login">Log in</a><a class="btn btn-dark btn-sm" href="/signup">Create account</a>`
    }</div>
  </div></header>`;
}

function appHeader(active) {
  const link = (href, label, key) => `<a href="${href}" class="${active === key ? "active" : ""}">${label}</a>`;
  return `<header class="header"><div class="container">
    ${logo("/new")}
    <nav class="nav app-nav">
      ${link("/new", "New interview", "new")}${link("/history", "History", "history")}${link("/settings", "Settings", "settings")}
    </nav>
    <div class="header-right"><a class="user-chip" href="/settings"><span class="name">${esc(firstName(me.name))}</span><span class="avatar">${esc(initials(me.name))}</span></a></div>
  </div></header>`;
}

function footer() {
  return `<footer class="footer"><div class="container">
    <span>InterviewAI — a student project by Team Tribe, Astana IT University</span>
    <nav><a href="/privacy">Privacy</a><a href="/help">Help</a></nav>
  </div></footer>`;
}

// Waveform bars. `levels` are 0..1; bars past `played` are drawn grey.
function waveHTML(n, cls = "") {
  return `<div class="wave ${cls}">${"<i></i>".repeat(n)}</div>`;
}
function setWave(el, levels, played = levels.length, maxH) {
  const bars = el.children;
  const h = maxH || el.clientHeight || 30;
  for (let i = 0; i < bars.length; i++) {
    const v = levels[i] ?? 0;
    bars[i].style.height = Math.max(3, v * h) + "px";
    bars[i].classList.toggle("off", i >= played);
  }
}
function fakeLevels(n, seed = 1) {
  let x = seed;
  const rnd = () => ((x = (x * 9301 + 49297) % 233280) / 233280);
  return Array.from({ length: n }, (_, i) => {
    const env = Math.sin((i / n) * Math.PI) * 0.65 + 0.3;
    return Math.min(1, 0.15 + env * (0.4 + rnd() * 0.6));
  });
}

// Live microphone level meter, shared by setup and interview pages.
async function openMic() {
  const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
  const ctx = new (window.AudioContext || window.webkitAudioContext)();
  const src = ctx.createMediaStreamSource(stream);
  const an = ctx.createAnalyser();
  an.fftSize = 512;
  src.connect(an);
  const buf = new Uint8Array(an.fftSize);
  return {
    level() {
      an.getByteTimeDomainData(buf);
      let sum = 0;
      for (const v of buf) sum += ((v - 128) / 128) ** 2;
      return Math.min(1, Math.sqrt(sum / buf.length) * 4.5);
    },
    close() {
      stream.getTracks().forEach((t) => t.stop());
      ctx.close();
    },
  };
}

// Drives a scrolling waveform from a mic: newest level on the right.
function animateWave(el, mic, opts = {}) {
  const n = el.children.length;
  const levels = new Array(n).fill(0.05);
  let raf, last = 0;
  const tick = (t) => {
    raf = requestAnimationFrame(tick);
    if (t - last < (opts.every || 70)) return;
    last = t;
    levels.shift();
    levels.push(opts.paused?.() ? 0.04 : Math.max(0.06, mic.level()));
    setWave(el, levels, n);
  };
  raf = requestAnimationFrame(tick);
  return () => cancelAnimationFrame(raf);
}

// ---------- router ----------
const routes = [
  [/^\/$/, landing],
  [/^\/signup$/, () => authPage("signup")],
  [/^\/login$/, () => authPage("login")],
  [/^\/sample$/, () => reportPage("sample")],
  [/^\/privacy$/, () => staticPage("privacy")],
  [/^\/help$/, () => staticPage("help")],
  [/^\/new$/, setupPage, true],
  [/^\/interview\/(\w+)$/, interviewPage, true],
  [/^\/report\/(\w+)$/, reportPage, true],
  [/^\/history$/, historyPage, true],
  [/^\/settings$/, settingsPage, true],
];

async function route() {
  cleanups.forEach((fn) => { try { fn(); } catch {} });
  cleanups = [];
  if ("speechSynthesis" in window) speechSynthesis.cancel();
  document.body.classList.remove("has-tabbar");
  const path = location.pathname;
  for (const [re, fn, needsAuth] of routes) {
    const m = path.match(re);
    if (!m) continue;
    if (needsAuth && !me) return go("/login?next=" + encodeURIComponent(path + location.search), true);
    if (needsAuth) document.body.classList.add("has-tabbar");
    window.scrollTo(0, 0);
    await fn(m[1]);
    if (location.hash) document.getElementById(location.hash.slice(1))?.scrollIntoView();
    return;
  }
  app.innerHTML = publicHeader() + `<main class="container page"><h1>Page not found</h1><p class="muted">Try the <a href="/">home page</a>.</p></main>`;
}

// ---------- landing ----------
function landing() {
  document.title = "InterviewAI — practice job interviews out loud";
  app.innerHTML = `${publicHeader()}
  <section class="hero"><div class="container">
    <div>
      <div class="eyebrow"><span class="logo-dot"></span>Your interviewer asks</div>
      <h1>“Tell me about a time you solved a difficult problem.”</h1>
      <p class="lede">Practice real job interviews out loud with an AI interviewer. Get a score and clear advice for every answer, and see how you improve.</p>
      <div class="hero-cta">
        <a class="btn btn-accent btn-lg" href="${startInterviewHref()}">Start a practice interview</a>
        <a class="btn btn-outline btn-lg" href="/sample">See a sample report</a>
      </div>
      <div class="mini-player"><button class="rec-btn" aria-label="Demo recording" tabindex="-1"></button>${waveHTML(52)}<span class="time" id="demo-time">0:42</span></div>
    </div>
    <div class="card score-card">
      <div class="top"><span>Answer 3 of 5</span><span class="badge badge-green">Good answer</span></div>
      <div class="big-score">78 <small>/ 100</small></div>
      <p class="desc">You explained the problem and your actions clearly. Add the result in numbers: how much faster the page loaded after your fix.</p>
      <div class="kv"><b>Strength</b><span>Clear structure: situation, task, action</span><b>Missing</b><span>The result and what you learned</span><b>Try next</b><span>End with one sentence about impact</span></div>
    </div>
  </div></section>

  <section class="section" id="how"><div class="container">
    <h2>How it works</h2>
    <p class="sub">One practice session takes about 15 minutes and has three steps.</p>
    <div class="steps">
      <div class="step"><div class="n">1</div><h3>Set up the interview</h3><p>Choose the job you are preparing for, your level, the interview type and how hard it should be.</p>
        <div class="pills"><span class="pill">Profession</span><span class="pill">Experience level</span><span class="pill">Interview type</span><span class="pill">Difficulty</span></div></div>
      <div class="step"><div class="n">2</div><h3>Answer out loud</h3><p>The AI asks each question by voice. You answer as in a real interview, and it may ask a follow-up if something is missing.</p>
        <div class="pills"><span class="pill">Voice questions</span><span class="pill">Speech-to-text</span><span class="pill">Follow-up questions</span></div></div>
      <div class="step"><div class="n">3</div><h3>Read your report</h3><p>Every answer gets a score from 0 to 100 with a reason, plus your strengths, weak spots and what to practice next.</p>
        <div class="pills"><span class="pill">Final score</span><span class="pill">Recommendations</span><span class="pill">Progress over time</span></div></div>
    </div>
  </div></section>

  <section class="section" id="who"><div class="container who">
    <img src="/img/interview.jpg" alt="A candidate answering questions in a job interview" loading="lazy">
    <div>
      <h2>Made for your first real interviews</h2>
      <p class="muted" style="margin-top:12px">If you know the theory but freeze when you have to say it out loud, this is for you.</p>
      <div class="who-item"><h3>Students</h3><p>Preparing for internships, graduate programs or a first professional job.</p></div>
      <div class="who-item"><h3>Graduates and juniors</h3><p>You have the knowledge and need practice saying it clearly under pressure.</p></div>
      <div class="who-item"><h3>Career changers</h3><p>Learn the typical questions and expectations of a new field before you apply.</p></div>
    </div>
  </div></section>

  <section class="cta-band"><div class="container">
    <div><h2>Your next interview is a<br>conversation. Rehearse it.</h2><p>Works in your browser. You only need a microphone.</p></div>
    <a class="btn btn-accent btn-lg" href="${startInterviewHref()}">Start a practice interview</a>
  </div></section>
  ${footer()}`;

  // Gently animated demo waveform.
  const wave = $(".mini-player .wave");
  const base = fakeLevels(52, 7);
  let played = 34, sec = 42;
  setWave(wave, base, played, 30);
  const iv = setInterval(() => {
    played = played >= 52 ? 8 : played + 1;
    if (played === 8) sec = 30;
    sec++;
    setWave(wave, base.map((v) => v * (0.85 + Math.random() * 0.3)), played, 30);
    $("#demo-time").textContent = `0:${String(sec % 60).padStart(2, "0")}`;
  }, 900);
  onLeave(() => clearInterval(iv));
}

// ---------- auth ----------
function authPage(mode) {
  if (me) return go(new URLSearchParams(location.search).get("next") || "/new", true);
  const signup = mode === "signup";
  const next = new URLSearchParams(location.search).get("next") || "/new";
  const q = location.search;
  document.title = (signup ? "Create account" : "Log in") + " — InterviewAI";
  app.innerHTML = `<div class="auth">
    <div class="auth-form"><div class="inner">
      ${logo()}
      <div class="tabs"><a href="/signup${q}" class="${signup ? "active" : ""}">Create account</a><a href="/login${q}" class="${signup ? "" : "active"}">Log in</a></div>
      <h1>${signup ? "Create your account" : "Welcome back"}</h1>
      <p class="intro">${signup ? "Your interviews and scores are saved, so you can track your progress." : "Log in to continue practising and see your progress."}</p>
      <form id="auth-form" novalidate>
        ${signup ? `<div class="field"><label for="name">Name</label><input class="input" id="name" name="name" autocomplete="name" placeholder="Aruzhan Sadykova" required></div>` : ""}
        <div class="field"><label for="email">Email</label><input class="input" id="email" name="email" type="email" autocomplete="email" placeholder="you@example.com" required></div>
        <div class="field"><label for="password">Password</label>
          <div class="pw-wrap"><input class="input" id="password" name="password" type="password" autocomplete="${signup ? "new-password" : "current-password"}" required><button type="button" class="pw-toggle">Show</button></div>
          ${signup ? `<div class="hint" id="pw-hint">At least 10 characters, a number and a symbol</div>` : ""}
        </div>
        <p class="form-error" id="form-error"></p>
        <button class="btn btn-dark btn-lg btn-block" type="submit">${signup ? "Create account" : "Log in"}</button>
      </form>
      <p class="fine">We save transcripts of your answers, not audio recordings. You can delete your history at any time in Settings.</p>
    </div></div>
    <div class="auth-photo" style="background-image:url(/img/signup.jpg)">
      <div class="photo-quote"><small>Your first practice question</small><p>“Walk me through a project you are proud of. What was your role?”</p></div>
    </div>
  </div>`;

  const pw = $("#password");
  $(".pw-toggle").onclick = (e) => {
    pw.type = pw.type === "password" ? "text" : "password";
    e.target.textContent = pw.type === "password" ? "Show" : "Hide";
  };
  if (signup) {
    pw.oninput = () => {
      const v = pw.value, hint = $("#pw-hint");
      const ok = v.length >= 10 && /\d/.test(v) && /[^\p{L}\d\s]/u.test(v);
      hint.className = "hint " + (v ? (ok ? "ok" : "bad") : "");
      hint.textContent = ok ? `Strong password: ${v.length} characters, a number and a symbol` : "At least 10 characters, a number and a symbol";
    };
  }
  $("#auth-form").onsubmit = async (e) => {
    e.preventDefault();
    const f = new FormData(e.target);
    const btn = e.target.querySelector("button[type=submit]");
    btn.disabled = true;
    $("#form-error").textContent = "";
    try {
      me = await api("POST", signup ? "/api/auth/register" : "/api/auth/login", Object.fromEntries(f));
      go(next.startsWith("/") ? next : "/new", true);
    } catch (err) {
      $("#form-error").textContent = err.message[0].toUpperCase() + err.message.slice(1) + ".";
      btn.disabled = false;
    }
  };
}

// ---------- setup ----------
const PROFESSIONS = ["Frontend developer", "Backend developer", "Data analyst", "QA engineer", "Project manager", "UX designer", "Marketing specialist"];
const DIFFS = [["Easy", "Common questions, no follow-ups"], ["Medium", "Some follow-up questions"], ["Hard", "Deep follow-ups, stricter scoring"]];

function setupPage() {
  document.title = "New interview — InterviewAI";
  const qs = new URLSearchParams(location.search);
  let saved = {};
  try { saved = JSON.parse(localStorage.getItem("iai-setup") || "{}"); } catch {}
  const st = {
    role: qs.get("role") || saved.role || "Frontend developer",
    level: qs.get("level") || saved.level || "Junior",
    type: qs.get("type") || saved.type || "Mixed",
    difficulty: qs.get("difficulty") || saved.difficulty || "Medium",
  };
  let other = !PROFESSIONS.includes(st.role);
  let mic = null, stopWave = null;

  app.innerHTML = `${appHeader("new")}
  <main class="container page"><div class="setup">
    <div>
      <h1>Set up your interview</h1>
      <p class="muted">Choose what you are preparing for. You can change it before every session.</p>
      <div class="setup-group"><h3>Profession</h3><p class="muted">The questions are written for this role.</p><div class="chips" id="roles"></div></div>
      <div class="setup-group"><h3>Experience level</h3><p class="muted">We adjust the depth of the questions.</p><div class="chips" data-key="level"></div></div>
      <div class="setup-group"><h3>Interview type</h3><p class="muted">Technical questions test knowledge; behavioral questions ask about your experience.</p><div class="chips" data-key="type"></div></div>
      <div class="setup-group"><h3>Difficulty</h3><p class="muted">Harder interviews ask more follow-up questions.</p><div class="diff" id="diff"></div></div>
    </div>
    <aside class="summary">
      <h2>Your interview</h2>
      <div class="kv" id="sum"></div>
      <div class="mic-box">
        <div class="row"><span>Microphone</span><span class="mic-state idle" id="mic-state">Not checked</span></div>
        ${waveHTML(34, "green")}
      </div>
      <button class="btn btn-accent btn-lg btn-block" id="start">Start interview</button>
      <p class="fine">Answer out loud, as in a real interview. If your microphone stops working, you can type an answer instead.</p>
    </aside>
  </div></main>`;

  const opts = { level: ["Intern", "Junior", "Middle"], type: ["Technical", "Behavioral", "Mixed"] };
  const draw = () => {
    $("#roles").innerHTML =
      PROFESSIONS.map((p) => `<button class="chip ${!other && st.role === p ? "on" : ""}" data-role="${p}">${p}</button>`).join("") +
      (other
        ? `<input class="chip-input" id="other" placeholder="Type a profession" maxlength="60" value="${esc(PROFESSIONS.includes(st.role) ? "" : st.role)}">`
        : `<button class="chip" data-role="__other">Other…</button>`);
    $$(".chips[data-key]").forEach((el) => {
      const k = el.dataset.key;
      el.innerHTML = opts[k].map((o) => `<button class="chip ${st[k] === o ? "on" : ""}" data-v="${o}">${o}</button>`).join("");
    });
    $("#diff").innerHTML = DIFFS.map(([d, s]) => `<button class="${st.difficulty === d ? "on" : ""}" data-d="${d}"><b>${d}</b><span>${s}</span></button>`).join("");
    $("#sum").innerHTML = `<span>Role</span><b>${esc(st.role || "—")}</b><span>Level</span><b>${st.level}</b><span>Type</span><b>${st.type}</b><span>Difficulty</span><b>${st.difficulty}</b><span>Questions</span><b>5, about 15 minutes</b>`;
  };
  draw();

  $("main").addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (!b) return;
    if (b.dataset.role === "__other") { other = true; st.role = ""; draw(); $("#other").focus(); return; }
    if (b.dataset.role) { other = false; st.role = b.dataset.role; }
    else if (b.dataset.v) st[b.closest("[data-key]").dataset.key] = b.dataset.v;
    else if (b.dataset.d) st.difficulty = b.dataset.d;
    else return;
    draw();
  });
  $("main").addEventListener("input", (e) => {
    if (e.target.id === "other") { st.role = e.target.value.trim(); $("#sum b").textContent = st.role || "—"; }
  });

  // Microphone check: runs automatically if permission was already granted.
  const micEl = $("#mic-state"), wave = $(".mic-box .wave");
  setWave(wave, new Array(34).fill(0.1), 34, 22);
  const checkMic = async () => {
    try {
      mic = await openMic();
      micEl.className = "mic-state ok"; micEl.textContent = "Working";
      stopWave = animateWave(wave, mic, { every: 60 });
    } catch {
      micEl.className = "mic-state bad"; micEl.textContent = "Not available";
    }
  };
  navigator.permissions?.query({ name: "microphone" }).then((p) => {
    if (p.state === "granted") checkMic();
    else { micEl.innerHTML = `<a href="#" id="check-mic">Check microphone</a>`; $("#check-mic").onclick = (e) => { e.preventDefault(); checkMic(); }; }
  }).catch(() => { micEl.innerHTML = `<a href="#" id="check-mic">Check microphone</a>`; $("#check-mic").onclick = (e) => { e.preventDefault(); checkMic(); }; });
  onLeave(() => { stopWave?.(); mic?.close(); });

  $("#start").onclick = async (e) => {
    if (!st.role) { toast("Please choose or type a profession."); return; }
    try { localStorage.setItem("iai-setup", JSON.stringify(st)); } catch {}
    const btn = e.currentTarget;
    btn.disabled = true;
    btn.innerHTML = `<span class="spinner" style="border-top-color:#fff"></span> Preparing your questions…`;
    try {
      const iv = await api("POST", "/api/interviews", st);
      go("/interview/" + iv.id);
    } catch (err) {
      toast(err.message);
      btn.disabled = false;
      btn.textContent = "Start interview";
    }
  };
}

// ---------- interview ----------
const SR = window.SpeechRecognition || window.webkitSpeechRecognition;

async function interviewPage(id) {
  app.innerHTML = `<div class="loading-page"><div class="spinner"></div></div>`;
  let iv;
  try { iv = await api("GET", "/api/interviews/" + id); } catch { return go("/history", true); }
  if (iv.status === "finished") return go("/report/" + id, true);
  document.title = "Interview — InterviewAI";
  document.body.classList.remove("has-tabbar");

  let mic = null, stopWave = null, rec = null;
  let recording = false, typing = !SR, busy = false;
  let finalText = "", interimText = "", answerStart = 0, silenceTimer = null, answerTick = null;

  try { mic = await openMic(); } catch { typing = true; }
  onLeave(() => {
    stopRecording(false);
    stopWave?.(); mic?.close();
    clearInterval(answerTick);
  });

  const started = new Date(iv.createdAt).getTime();
  const clock = setInterval(() => { const el = $("#iv-clock"); if (el) el.textContent = mmss((Date.now() - started) / 1000); }, 1000);
  onLeave(() => clearInterval(clock));

  const current = () => iv.turns[iv.turns.length - 1];

  function render() {
    const t = current();
    const mainIdx = t.main;
    const titles = [];
    for (const turn of iv.turns) if (!turn.followUp) titles[turn.main - 1] = { title: turn.title, score: turn.score };
    iv.planned.forEach((p, i) => (titles[iv.turns.filter((x) => !x.followUp).length + i] = { title: p.title }));
    const prog = Array.from({ length: iv.mainTotal }, (_, i) => `<i class="${i + 1 < mainIdx ? "done" : i + 1 === mainIdx ? "cur" : ""}"></i>`).join("");

    app.innerHTML = `<header class="header iv-header"><div class="container">
      ${logo("/new")}
      <span class="iv-meta">${esc(iv.role)} · ${iv.level} · ${iv.type} · ${iv.difficulty}</span>
      <div class="progress">${prog}</div>
      <span class="iv-count"><b>Question ${mainIdx}</b> of ${iv.mainTotal}</span>
      <span class="time" id="iv-clock">${mmss((Date.now() - started) / 1000)}</span>
      <button class="btn btn-outline btn-sm" id="end">End interview</button>
    </div></header>
    <main class="container interview">
      <div>
        <div class="who-asks"><span class="logo-dot"></span>Interviewer ${t.followUp ? `<span class="badge badge-grey">Follow-up question</span>` : ""}</div>
        <h1 class="question">${esc(t.text)}</h1>
        <p class="q-context">${t.followUp && t.context ? `Follow-up to ${esc(t.context.startsWith("your") ? "" : "your answer about ")}<b>${esc(t.context)}</b>.` : `${t.kind === "technical" ? "Technical" : "Behavioral"} question`}</p>
        <div class="answer-area" id="answer-area"></div>
      </div>
      <aside class="side">
        <h3>Questions</h3>
        <ol class="qlist">${titles.map((q, i) => {
          const n = i + 1, cls = n < mainIdx ? "" : n === mainIdx ? "cur" : "todo";
          const sc = n < mainIdx && q?.score != null ? `<span class="badge ${badgeClass(q.score)}">${q.score}</span>` : "";
          return `<li class="${cls}"><span class="num">${n}</span><span class="t">${esc(q?.title || "")}</span>${sc}</li>`;
        }).join("")}</ol>
        ${t.tip ? `<p class="tip"><b>Tip for this answer.</b> ${esc(t.tip)}</p>` : ""}
      </aside>
    </main>`;
    $("#end").onclick = endInterview;
    renderAnswerArea();
  }

  function renderAnswerArea() {
    const area = $("#answer-area");
    stopWave?.(); stopWave = null;
    if (busy) {
      area.innerHTML = `<div class="thinking"><span class="spinner"></span>Scoring your answer…</div>`;
      return;
    }
    if (typing) {
      area.innerHTML = `
        <textarea class="answer-text" id="typed" placeholder="Type your answer as you would say it…">${esc(finalText)}</textarea>
        <div class="controls">
          <span class="help">${!SR ? "Voice answers need Chrome, Edge or Safari. You can type your answer here." : !mic ? "Microphone not available. You can type your answer here." : "Typing instead of speaking."}</span>
          <div class="btns">
            <button class="btn btn-outline" id="repeat">Repeat question</button>
            ${SR && mic ? `<button class="btn btn-outline" id="voice">Answer by voice</button>` : ""}
            <button class="btn btn-dark" id="submit-typed">Submit answer</button>
          </div>
        </div>`;
      $("#typed").focus();
      if (!answerStart) answerStart = Date.now();
      $("#submit-typed").onclick = () => submit($("#typed").value);
      $("#voice") && ($("#voice").onclick = () => { finalText = $("#typed").value; typing = false; renderAnswerArea(); startRecording(); });
      $("#repeat").onclick = () => speak(current().text);
      return;
    }
    area.innerHTML = `
      <div class="listen-row"><span class="listen-state ${recording ? "on" : ""}" id="lstate">${recording ? "Listening" : "Ready"}</span><span class="time" id="atime">00:00</span></div>
      ${waveHTML(64)}
      <div class="transcript" id="transcript"></div>
      <div class="controls">
        <button class="big-rec ${recording ? "" : "start"}" id="rec" aria-label="${recording ? "Finish answer" : "Start answering"}"></button>
        <span class="help" id="rec-help"></span>
        <div class="btns"><button class="btn btn-outline" id="repeat">Repeat question</button><button class="btn btn-outline" id="type">Type instead</button></div>
      </div>`;
    stopWave = animateWave($("#answer-area .wave"), mic, { paused: () => !recording });
    drawTranscript();
    $("#rec").onclick = () => (recording ? finishSpoken() : startRecording());
    $("#type").onclick = () => { stopRecording(false); typing = true; renderAnswerArea(); };
    $("#repeat").onclick = async () => { const was = recording; stopRecording(false); renderAnswerArea(); await speak(current().text); if (was) startRecording(); };
  }

  function drawTranscript() {
    const el = $("#transcript");
    if (!el) return;
    if (!finalText && !interimText) {
      el.innerHTML = `<span class="placeholder">${recording ? "Start speaking — your words will appear here." : "Press the red button and answer out loud."}</span>`;
    } else {
      el.innerHTML = `${esc(finalText)} <span class="interim">${esc(interimText)}</span>`;
    }
    const help = $("#rec-help");
    if (help) help.textContent = recording ? "Press when you finish your answer, or wait 4 seconds of silence" : "Press to start answering";
  }

  function startRecording() {
    if (!SR || recording) return;
    if ("speechSynthesis" in window) speechSynthesis.cancel();
    recording = true;
    if (!answerStart) answerStart = Date.now();
    rec = new SR();
    rec.lang = "en-US";
    rec.continuous = true;
    rec.interimResults = true;
    rec.onresult = (e) => {
      interimText = "";
      for (let i = e.resultIndex; i < e.results.length; i++) {
        const r = e.results[i];
        if (r.isFinal) finalText = (finalText + " " + r[0].transcript.trim()).trim();
        else interimText += r[0].transcript;
      }
      drawTranscript();
      armSilence();
    };
    rec.onerror = (e) => {
      if (e.error === "not-allowed" || e.error === "service-not-allowed") {
        recording = false; typing = true; toast("Speech recognition is blocked. You can type your answer instead."); renderAnswerArea();
      }
    };
    // Chrome stops recognition on its own after a pause; restart while we still want to listen.
    rec.onend = () => { if (recording) try { rec.start(); } catch {} };
    try { rec.start(); } catch {}
    clearInterval(answerTick);
    answerTick = setInterval(() => { const el = $("#atime"); if (el) el.textContent = mmss((Date.now() - answerStart) / 1000); }, 500);
    renderAnswerArea();
  }

  function armSilence() {
    clearTimeout(silenceTimer);
    silenceTimer = setTimeout(() => { if (recording && finalText.trim()) finishSpoken(); }, 4000);
  }

  function stopRecording(redraw = true) {
    clearTimeout(silenceTimer);
    if (!recording) return;
    recording = false;
    try { rec.onend = null; rec.stop(); } catch {}
    rec = null;
    if (redraw) renderAnswerArea();
  }

  function finishSpoken() {
    const text = (finalText + " " + interimText).trim();
    stopRecording(false);
    if (!text) { toast("We did not hear anything. Try again or type your answer."); renderAnswerArea(); return; }
    submit(text);
  }

  async function submit(text) {
    if (busy) return;
    text = (text || "").trim();
    if (!text && !confirm("Skip this question without an answer?")) return;
    const seconds = answerStart ? Math.round((Date.now() - answerStart) / 1000) : 0;
    busy = true;
    clearInterval(answerTick);
    renderAnswerArea();
    try {
      iv = await api("POST", `/api/interviews/${id}/answer`, { text, seconds });
    } catch (err) {
      busy = false;
      finalText = text;
      typing = true;
      toast(err.message);
      renderAnswerArea();
      return;
    }
    busy = false;
    finalText = interimText = "";
    answerStart = 0;
    if (iv.status === "finished") return go("/report/" + id, true);
    if (!SR || !mic) typing = true;
    else typing = false;
    render();
    await speak(current().text);
    if (!typing && location.pathname === "/interview/" + id) startRecording();
  }

  async function endInterview() {
    if (!confirm("End the interview now? Questions you have answered will be scored.")) return;
    stopRecording(false);
    busy = true;
    renderAnswerArea();
    const res = await api("POST", `/api/interviews/${id}/finish`).catch((e) => ({ error: e.message }));
    if (res.deleted) { toast("Interview ended. No answers were saved."); return go("/new", true); }
    if (res.error) { busy = false; toast(res.error); renderAnswerArea(); return; }
    go("/report/" + id, true);
  }

  render();
  // Read the first (or resumed) question aloud, then start listening.
  await speak(current().text);
  if (!typing && location.pathname === "/interview/" + id) startRecording();
}

function speak(text) {
  return new Promise((resolve) => {
    if (!("speechSynthesis" in window)) return resolve();
    speechSynthesis.cancel();
    const u = new SpeechSynthesisUtterance(text);
    u.lang = "en-US";
    u.rate = 1;
    const voices = speechSynthesis.getVoices().filter((v) => v.lang.startsWith("en"));
    u.voice = voices.find((v) => /natural|google us|samantha|aria|jenny/i.test(v.name)) || voices[0] || null;
    let done = false;
    const finish = () => { if (!done) { done = true; resolve(); } };
    u.onend = finish;
    u.onerror = finish;
    setTimeout(finish, 2500 + text.length * 90); // safety net if onend never fires
    speechSynthesis.speak(u);
  });
}

// ---------- report ----------
async function reportPage(id) {
  const sample = id === "sample";
  if (!sample) document.body.classList.add("has-tabbar");
  app.innerHTML = (me && !sample ? appHeader("history") : publicHeader()) + `<div class="loading-page"><div class="spinner"></div></div>`;
  let iv;
  try { iv = await api("GET", sample ? "/api/sample-report" : "/api/interviews/" + id); } catch { return go("/history", true); }
  if (!iv.report) return go("/interview/" + id, true);
  const r = iv.report;
  document.title = (sample ? "Sample report" : "Your feedback report") + " — InterviewAI";

  const mins = Math.max(1, Math.round((new Date(iv.finishedAt) - new Date(iv.createdAt)) / 60000));
  const delta = iv.prevScore != null ? r.finalScore - iv.prevScore : null;
  const again = `/new?${new URLSearchParams({ role: iv.role, level: iv.level, type: iv.type, difficulty: iv.difficulty })}`;
  const li = (xs) => (xs || []).map((x) => `<li>${esc(x)}</li>`).join("");

  app.innerHTML = `${me && !sample ? appHeader("history") : publicHeader()}
  <main class="container page">
    <div class="report-head">
      <div class="crumbs">${sample ? "Sample report" : `<a href="/history">History</a> / ${dateLabel(iv.createdAt) === "Today" ? "Today’s interview" : dateLabel(iv.createdAt)}, ${mins} minute${mins === 1 ? "" : "s"}`}</div>
      <div class="page-head">
        <div><h1>${sample ? "A sample feedback report" : "Your feedback report"}</h1>
        <p class="muted">${esc(iv.role)} · ${iv.level} · ${iv.type} interview · ${iv.difficulty} difficulty · ${iv.turns.length} questions</p></div>
        <div class="btns">
          <button class="btn btn-outline" id="pdf">Download PDF</button>
          <a class="btn btn-dark" href="${sample ? startInterviewHref() : again}">${sample ? "Try it yourself" : "Practice again"}</a>
        </div>
      </div>
    </div>
    <div class="report-top">
      <div>
        <div class="final-label">Final score</div>
        <div class="final-score">${r.finalScore} <small>/ 100</small></div>
        ${delta != null ? `<p class="delta ${delta < 0 ? "down" : ""}"><b>${delta >= 0 ? "+" : "−"}${Math.abs(delta)}</b> since your last interview</p>` : `<p class="delta">Your first interview — a baseline to beat</p>`}
      </div>
      <div>
        <p class="report-summary">${esc(r.summary)}</p>
        <div class="three">
          <div class="box g"><h4>Strengths</h4><ul>${li(r.strengths)}</ul></div>
          <div class="box r"><h4>Weak spots</h4><ul>${li(r.weakSpots)}</ul></div>
          <div class="box"><h4>Practice next</h4><ul>${li(r.practiceNext)}</ul></div>
        </div>
      </div>
    </div>
    <section class="answers">
      <h2>Answer by answer</h2>
      <p class="muted">Each answer is scored from 0 to 100. The reason explains the score.</p>
      ${iv.turns.map((t, i) => `<div class="ans">
        <span class="n">${i + 1}</span>
        <div>
          <h3>${esc(t.text)} ${t.followUp ? `<span class="badge badge-grey">Follow-up</span>` : ""}</h3>
          <p class="reason"><b>Reason:</b> ${esc(t.reason)}</p>
          ${t.answer ? `<details><summary>Your answer${t.seconds ? ` · ${mmss(t.seconds)}` : ""}</summary><p>${esc(t.answer)}</p></details>` : ""}
        </div>
        <div><div class="sc">${t.score}</div><div class="bar"><i class="${scoreClass(t.score)}" style="width:${t.score}%"></i></div></div>
      </div>`).join("")}
    </section>
  </main>`;
  $("#pdf").onclick = () => { $$("details").forEach((d) => (d.open = true)); window.print(); };
}

// ---------- history & progress ----------
async function historyPage() {
  document.title = "Your progress — InterviewAI";
  app.innerHTML = appHeader("history") + `<div class="loading-page"><div class="spinner"></div></div>`;
  const all = await api("GET", "/api/interviews").catch(() => []);
  const done = all.filter((x) => x.score != null);
  let filter = "All";

  if (!all.length) {
    app.innerHTML = `${appHeader("history")}<main class="container page">
      <h1>Your progress</h1>
      <div class="empty" style="margin-top:30px"><h2>No interviews yet</h2><p>Your scores and reports will appear here after your first practice session.</p><a class="btn btn-accent btn-lg" href="/new">Start your first interview</a></div>
    </main>`;
    return;
  }

  const chrono = [...done].reverse();
  const first = chrono[0]?.score, last = chrono[chrono.length - 1]?.score;
  const words = ["Zero", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten"];
  const countWord = (n) => (n <= 10 ? words[n] : String(n));
  let subtitle = `${countWord(done.length)} interview${done.length === 1 ? "" : "s"} so far.`;
  if (done.length > 1) {
    const d = last - first;
    subtitle += d > 0 ? ` Your score went up by ${d} point${d === 1 ? "" : "s"}.` : d < 0 ? ` Your score went down by ${-d} points — keep practising.` : " Your score is holding steady.";
  }
  const avg = done.length ? Math.round(done.reduce((s, x) => s + x.score, 0) / done.length) : null;
  const answers = done.reduce((s, x) => s + (x.answers || 0), 0);
  const skills = { structure: [], impact: [], accuracy: [] };
  done.slice(0, 5).forEach((x) => x.skills && Object.keys(skills).forEach((k) => x.skills[k] != null && skills[k].push(x.skills[k])));
  const sk = (k) => (skills[k].length ? Math.round(skills[k].reduce((a, b) => a + b, 0) / skills[k].length) : null);
  const roles = [...new Set(done.map((x) => x.role))];

  app.innerHTML = `${appHeader("history")}
  <main class="container page">
    <div class="page-head">
      <div><h1>Your progress</h1><p class="muted">${subtitle}</p></div>
      <a class="btn btn-accent" href="/new">Start a new interview</a>
    </div>
    ${done.length ? `<div class="prog-grid">
      <div class="card chart-card">
        <div class="head"><h2>Final score per interview</h2><span>${roles.length === 1 ? esc(roles[0]) : "All roles"}, all types</span></div>
        ${chartSVG(chrono.slice(-8))}
      </div>
      <div class="stack">
        <div class="stat"><small>Average score</small><div class="v">${avg}</div><p>Across ${done.length} interview${done.length === 1 ? "" : "s"} and ${answers} answer${answers === 1 ? "" : "s"}</p></div>
        <div class="stat"><small>Skills to watch</small>
          ${[["structure", "Structure of answers"], ["impact", "Results and impact"], ["accuracy", "Technical accuracy"]].map(([k, label]) => {
            const v = sk(k);
            if (v == null) return "";
            const lowest = Math.min(...["structure", "impact", "accuracy"].map(sk).filter((x) => x != null));
            return `<div class="skill"><div class="row"><span>${label}</span><span>${v}</span></div><div class="bar"><i class="${v === lowest ? "low" : "dark"}" style="width:${v}%"></i></div></div>`;
          }).join("")}
        </div>
      </div>
    </div>` : `<div style="height:30px"></div>`}
    <div class="filter-row"><div class="chips" id="filters"></div><span class="muted" id="count" style="font-size:12.5px"></span></div>
    <div class="table-wrap"><table>
      <thead><tr><th>Date</th><th>Role and level</th><th>Type</th><th>Difficulty</th><th class="num">Questions</th><th class="num">Score</th><th></th></tr></thead>
      <tbody id="rows"></tbody>
    </table></div>
  </main>`;

  const draw = () => {
    $("#filters").innerHTML = ["All", "Technical", "Behavioral", "Mixed"].map((f) => `<button class="chip ${f === filter ? "on" : ""}" data-f="${f}">${f}</button>`).join("");
    const rows = all.filter((x) => filter === "All" || x.type === filter);
    $("#count").textContent = `${rows.length} interview${rows.length === 1 ? "" : "s"}`;
    $("#rows").innerHTML = rows.map((x) => `<tr>
      <td>${dateLabel(x.createdAt)}</td>
      <td><b>${esc(x.role)}</b> · ${x.level}</td><td>${x.type}</td><td>${x.difficulty}</td>
      <td class="num">${x.questions}</td>
      <td class="num score">${x.score ?? "—"}</td>
      <td class="num">${x.status === "finished" ? `<a href="/report/${x.id}">Open report</a>` : `<a href="/interview/${x.id}">Continue</a>`}</td>
    </tr>`).join("") || `<tr><td colspan="7" class="muted">No ${filter.toLowerCase()} interviews yet.</td></tr>`;
  };
  draw();
  $("#filters").onclick = (e) => { const b = e.target.closest("[data-f]"); if (b) { filter = b.dataset.f; draw(); } };
}

function chartSVG(points) {
  const W = 640, H = 230, L = 34, R = 20, T = 14, B = 34;
  const lo = Math.min(40, Math.floor((Math.min(...points.map((p) => p.score)) - 5) / 20) * 20);
  const y = (v) => T + (1 - (v - lo) / (100 - lo)) * (H - T - B);
  const x = (i) => (points.length === 1 ? (L + W - R) / 2 : L + 24 + (i * (W - R - L - 48)) / (points.length - 1));
  const grid = [];
  for (let v = lo; v <= 100; v += 20) grid.push(`<line x1="${L}" x2="${W - R}" y1="${y(v)}" y2="${y(v)}" stroke="#e6e4df"/><text x="${L - 10}" y="${y(v) + 4}" text-anchor="end" font-size="10" fill="#8a8a8f">${v}</text>`);
  const path = points.map((p, i) => `${i ? "L" : "M"}${x(i)},${y(p.score)}`).join(" ");
  const lastI = points.length - 1;
  return `<svg viewBox="0 0 ${W} ${H}" role="img" aria-label="Final score per interview">
    ${grid.join("")}
    <line x1="${L}" x2="${W - R}" y1="${y(80)}" y2="${y(80)}" stroke="#2f7d5b" stroke-dasharray="4 4"/>
    <text x="${L + 6}" y="${y(80) - 6}" font-size="10" fill="#2f7d5b">Target 80</text>
    <path d="${path}" fill="none" stroke="#18181b" stroke-width="1.6"/>
    ${points.map((p, i) => `
      <circle cx="${x(i)}" cy="${y(p.score)}" r="${i === lastI ? 6 : 4.5}" fill="${i === lastI ? "#c9432a" : "#fff"}" stroke="${i === lastI ? "#c9432a" : "#18181b"}" stroke-width="1.6"/>
      <text x="${x(i)}" y="${y(p.score) - 12}" text-anchor="middle" font-family="Lora, serif" font-size="13" fill="${i === lastI ? "#c9432a" : "#18181b"}">${p.score}</text>
      <text x="${x(i)}" y="${H - 8}" text-anchor="middle" font-size="10" fill="#8a8a8f">${dateLabel(p.createdAt)}</text>`).join("")}
  </svg>`;
}

// ---------- settings ----------
function settingsPage() {
  document.title = "Settings — InterviewAI";
  app.innerHTML = `${appHeader("settings")}
  <main class="container page settings">
    <h1>Settings</h1>
    <p class="muted">${esc(me.email)}</p>
    <section><h2>Your name</h2><p class="muted">Shown in the app header.</p>
      <form class="inline-form" id="name-form"><input class="input" name="name" value="${esc(me.name)}" maxlength="80" required><button class="btn btn-dark">Save</button></form>
    </section>
    <section><h2>Interview history</h2><p class="muted">We keep transcripts of your answers and your scores, never audio recordings. Deleting your history removes all interviews and reports.</p>
      <button class="btn btn-danger" id="del-history">Delete my history</button>
    </section>
    <section><h2>Account</h2><p class="muted">Log out on this device, or delete your account and everything in it.</p>
      <div style="display:flex;gap:10px;flex-wrap:wrap"><button class="btn btn-outline" id="logout">Log out</button><button class="btn btn-danger" id="del-account">Delete account</button></div>
    </section>
  </main>`;
  $("#name-form").onsubmit = async (e) => {
    e.preventDefault();
    try { me = await api("PATCH", "/api/me", { name: new FormData(e.target).get("name") }); toast("Name saved."); settingsPage(); }
    catch (err) { toast(err.message); }
  };
  $("#del-history").onclick = async () => {
    if (!confirm("Delete all your interviews and reports? This cannot be undone.")) return;
    await api("DELETE", "/api/interviews");
    toast("Your history was deleted.");
  };
  $("#logout").onclick = async () => { await api("POST", "/api/auth/logout"); me = null; go("/", true); };
  $("#del-account").onclick = async () => {
    if (!confirm("Delete your account and all your interviews? This cannot be undone.")) return;
    await api("DELETE", "/api/me");
    me = null;
    toast("Your account was deleted.");
    go("/", true);
  };
}

// ---------- static pages ----------
function staticPage(which) {
  const pages = {
    privacy: ["Privacy", `
      <p>InterviewAI is a student project. We keep as little data as we can.</p>
      <h2>What we store</h2><p>Your name, email and a salted hash of your password. For every interview: the questions, the text transcript of your answers, scores and the feedback report.</p>
      <h2>What we do not store</h2><p>Audio. Speech is turned into text by your browser’s built-in speech recognition, and only the text is sent to our server.</p>
      <h2>AI processing</h2><p>When AI scoring is enabled, the questions and your answer transcripts are sent to Anthropic’s Claude API to write questions and feedback.</p>
      <h2>Deleting your data</h2><p>You can delete your interview history or your whole account at any time in Settings.</p>`],
    help: ["Help", `
      <h2>Which browser should I use?</h2><p>Voice answers work best in Chrome or Edge on desktop and Android, and Safari on iPhone. In other browsers you can type your answers.</p>
      <h2>The microphone does not work</h2><p>Allow microphone access when the browser asks. If you blocked it, click the lock icon in the address bar and allow the microphone, then reload the page.</p>
      <h2>How is my answer scored?</h2><p>Each answer gets a score from 0 to 100 with a reason. We look at structure, concrete examples, results and technical accuracy. The final score is the average of your answers.</p>
      <h2>What are follow-up questions?</h2><p>On Medium and Hard difficulty the interviewer may ask a follow-up when something important is missing, just like in a real interview.</p>`],
  };
  const [title, body] = pages[which];
  document.title = title + " — InterviewAI";
  app.innerHTML = `${publicHeader()}<main class="container page"><div class="prose"><h1>${title}</h1>${body}</div></main>${footer()}`;
}

// ---------- boot ----------
(async () => {
  try { me = await api("GET", "/api/me"); } catch { me = null; }
  route();
})();
