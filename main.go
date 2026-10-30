package main

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
)

//go:embed web
var webFS embed.FS

const cookieName = "iai_session"

type server struct {
	store *Store
	ai    *Evaluator
}

func main() {
	// Hosts such as Render pass the port to listen on in PORT.
	addr := envOr("ADDR", ":"+envOr("PORT", "8080"))
	store, err := OpenStore(envOr("DATA_FILE", "data/db.json"))
	if err != nil {
		log.Fatal(err)
	}
	s := &server{store: store, ai: NewEvaluator()}
	if err := ensureDemoUser(store); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/me", s.auth(s.me))
	mux.HandleFunc("PATCH /api/me", s.auth(s.updateMe))
	mux.HandleFunc("DELETE /api/me", s.auth(s.deleteMe))
	mux.HandleFunc("GET /api/sample-report", s.sampleReport)
	mux.HandleFunc("GET /api/interviews", s.auth(s.listInterviews))
	mux.HandleFunc("DELETE /api/interviews", s.auth(s.deleteInterviews))
	mux.HandleFunc("POST /api/interviews", s.auth(s.createInterview))
	mux.HandleFunc("GET /api/interviews/{id}", s.auth(s.getInterview))
	mux.HandleFunc("POST /api/interviews/{id}/answer", s.auth(s.answer))
	mux.HandleFunc("POST /api/interviews/{id}/finish", s.auth(s.finish))

	// Static files; any unknown non-API path falls back to the SPA shell.
	sub, _ := fs.Sub(webFS, "web")
	files := http.FileServerFS(sub)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if strings.HasPrefix(p, "api/") {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if p != "" {
			if _, err := fs.Stat(sub, p); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		b, _ := fs.ReadFile(sub, "index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})

	mode := "offline scoring (set ANTHROPIC_API_KEY to use Claude)"
	if s.ai.Enabled() {
		mode = "Claude " + s.ai.model
	}
	log.Printf("InterviewAI listening on http://localhost%s — %s", addr, mode)
	log.Fatal(http.ListenAndServe(addr, logRequests(mux)))
}

// ensureDemoUser creates the account from DEMO_EMAIL and DEMO_PASSWORD on
// every start, so a shared demo login survives hosts that wipe the data file.
// If the account already exists its password is reset to DEMO_PASSWORD.
func ensureDemoUser(store *Store) error {
	email, pw := strings.TrimSpace(os.Getenv("DEMO_EMAIL")), os.Getenv("DEMO_PASSWORD")
	if email == "" || pw == "" {
		return nil
	}
	salt := newID(16)
	hash := hashPassword(pw, salt)
	if u, err := store.UserByEmail(email); err == nil {
		return store.SetPassword(u.ID, salt, hash)
	}
	log.Printf("created demo account %s", email)
	return store.CreateUser(&User{Name: envOr("DEMO_NAME", "Demo Student"), Email: email, Salt: salt, PassHash: hash})
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	return json.NewDecoder(r.Body).Decode(v)
}

type userHandler func(w http.ResponseWriter, r *http.Request, u *User)

func (s *server) auth(h userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "please log in")
			return
		}
		u, err := s.store.UserBySession(c.Value)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "please log in")
			return
		}
		h(w, r, u)
	}
}

func publicUser(u *User) map[string]any {
	return map[string]any{"id": u.ID, "name": u.Name, "email": u.Email, "createdAt": u.CreatedAt}
}

func hashPassword(pw, saltHex string) string {
	salt, _ := hex.DecodeString(saltHex)
	key, _ := pbkdf2.Key(sha256.New, pw, salt, 210000, 32)
	return hex.EncodeToString(key)
}

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func checkPassword(pw string) error {
	var digit, symbol bool
	for _, r := range pw {
		switch {
		case unicode.IsDigit(r):
			digit = true
		case !unicode.IsLetter(r) && !unicode.IsSpace(r):
			symbol = true
		}
	}
	if len([]rune(pw)) < 10 || !digit || !symbol {
		return errors.New("password needs at least 10 characters, a number and a symbol")
	}
	return nil
}

func (s *server) startSession(w http.ResponseWriter, r *http.Request, u *User) {
	tok, err := s.store.NewSession(u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not start session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 60 * 60 * 24 * 30,
		// Behind a hosting proxy TLS ends before our server, so check the forwarded scheme too.
		Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	writeJSON(w, http.StatusOK, publicUser(u))
}

// ---------- auth handlers ----------

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Email, Password string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	in.Name, in.Email = strings.TrimSpace(in.Name), strings.TrimSpace(in.Email)
	switch {
	case in.Name == "":
		writeErr(w, http.StatusBadRequest, "please enter your name")
		return
	case !emailRe.MatchString(in.Email):
		writeErr(w, http.StatusBadRequest, "please enter a valid email")
		return
	}
	if err := checkPassword(in.Password); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	salt := newID(16)
	u := &User{Name: in.Name, Email: in.Email, Salt: salt, PassHash: hashPassword(in.Password, salt)}
	if err := s.store.CreateUser(u); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.startSession(w, r, u)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	u, err := s.store.UserByEmail(strings.TrimSpace(in.Email))
	if err != nil || subtle.ConstantTimeCompare([]byte(hashPassword(in.Password, u.Salt)), []byte(u.PassHash)) != 1 {
		writeErr(w, http.StatusUnauthorized, "wrong email or password")
		return
	}
	s.startSession(w, r, u)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.store.EndSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) me(w http.ResponseWriter, r *http.Request, u *User) {
	writeJSON(w, http.StatusOK, publicUser(u))
}

func (s *server) updateMe(w http.ResponseWriter, r *http.Request, u *User) {
	var in struct{ Name string }
	if err := readJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "please enter your name")
		return
	}
	s.store.UpdateUserName(u.ID, strings.TrimSpace(in.Name))
	u.Name = strings.TrimSpace(in.Name)
	writeJSON(w, http.StatusOK, publicUser(u))
}

func (s *server) deleteMe(w http.ResponseWriter, r *http.Request, u *User) {
	s.store.DeleteUser(u.ID)
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
