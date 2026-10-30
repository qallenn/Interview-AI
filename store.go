package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// User is a registered account. Only a salted password hash is stored.
type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	PassHash  string    `json:"passHash"`
	Salt      string    `json:"salt"`
	CreatedAt time.Time `json:"createdAt"`
}

// Turn is one question asked during an interview and the user's answer to it.
type Turn struct {
	Text     string `json:"text"`
	Title    string `json:"title"`             // short label shown in the sidebar
	Kind     string `json:"kind"`              // technical | behavioral
	FollowUp bool   `json:"followUp"`          // true when generated from a previous answer
	Context  string `json:"context,omitempty"` // what the follow-up refers to
	Tip      string `json:"tip,omitempty"`     // hint shown while answering
	Answer   string `json:"answer,omitempty"`  // transcript, never audio
	Seconds  int    `json:"seconds,omitempty"` // time spent answering
	Score    *int   `json:"score,omitempty"`   // 0..100, nil until answered
	Reason   string `json:"reason,omitempty"`  // why this score
	Main     int    `json:"main"`              // 1-based index of the main question it belongs to
}

// Report is the final feedback for a finished interview.
type Report struct {
	FinalScore   int            `json:"finalScore"`
	Summary      string         `json:"summary"`
	Strengths    []string       `json:"strengths"`
	WeakSpots    []string       `json:"weakSpots"`
	PracticeNext []string       `json:"practiceNext"`
	Skills       map[string]int `json:"skills"` // structure, impact, accuracy
}

type Interview struct {
	ID         string     `json:"id"`
	UserID     string     `json:"userId"`
	Role       string     `json:"role"`
	Level      string     `json:"level"`
	Type       string     `json:"type"`
	Difficulty string     `json:"difficulty"`
	Status     string     `json:"status"` // active | finished
	Turns      []Turn     `json:"turns"`
	Planned    []Turn     `json:"planned"` // main questions not asked yet
	MainTotal  int        `json:"mainTotal"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Report     *Report    `json:"report,omitempty"`
}

type dbData struct {
	Users      map[string]*User      `json:"users"`
	Sessions   map[string]string     `json:"sessions"` // token -> user id
	Interviews map[string]*Interview `json:"interviews"`
}

// Store is a small JSON-file database guarded by a mutex. Every write is
// flushed to disk atomically, which is plenty for an MVP with few users.
type Store struct {
	mu   sync.Mutex
	path string
	d    dbData
}

var errNotFound = errors.New("not found")

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, d: dbData{
		Users: map[string]*User{}, Sessions: map[string]string{}, Interviews: map[string]*Interview{},
	}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, os.MkdirAll(filepath.Dir(path), 0o755)
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.d); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) flush() error {
	b, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newID(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) CreateUser(u *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.d.Users {
		if strings.EqualFold(x.Email, u.Email) {
			return errors.New("an account with this email already exists")
		}
	}
	u.ID = newID(8)
	u.CreatedAt = time.Now()
	s.d.Users[u.ID] = u
	return s.flush()
}

func (s *Store) UserByEmail(email string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.d.Users {
		if strings.EqualFold(u.Email, email) {
			c := *u
			return &c, nil
		}
	}
	return nil, errNotFound
}

func (s *Store) UpdateUserName(id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.d.Users[id]
	if !ok {
		return errNotFound
	}
	u.Name = name
	return s.flush()
}

// DeleteUser removes the account, its sessions and all of its interviews.
func (s *Store) DeleteUser(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.d.Users, id)
	for t, uid := range s.d.Sessions {
		if uid == id {
			delete(s.d.Sessions, t)
		}
	}
	for k, iv := range s.d.Interviews {
		if iv.UserID == id {
			delete(s.d.Interviews, k)
		}
	}
	return s.flush()
}

func (s *Store) NewSession(userID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok := newID(32)
	s.d.Sessions[tok] = userID
	return tok, s.flush()
}

func (s *Store) EndSession(tok string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.d.Sessions, tok)
	return s.flush()
}

func (s *Store) UserBySession(tok string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.d.Users[s.d.Sessions[tok]]
	if !ok {
		return nil, errNotFound
	}
	c := *u
	return &c, nil
}

func (s *Store) SaveInterview(iv *Interview) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *iv
	s.d.Interviews[iv.ID] = &c
	return s.flush()
}

func (s *Store) Interview(userID, id string) (*Interview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	iv, ok := s.d.Interviews[id]
	if !ok || iv.UserID != userID {
		return nil, errNotFound
	}
	c := *iv
	c.Turns = append([]Turn(nil), iv.Turns...)
	c.Planned = append([]Turn(nil), iv.Planned...)
	return &c, nil
}

// Interviews returns the user's interviews, newest first.
func (s *Store) Interviews(userID string) []*Interview {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Interview
	for _, iv := range s.d.Interviews {
		if iv.UserID == userID {
			c := *iv
			out = append(out, &c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *Store) DeleteInterviews(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, iv := range s.d.Interviews {
		if iv.UserID == userID {
			delete(s.d.Interviews, k)
		}
	}
	return s.flush()
}
