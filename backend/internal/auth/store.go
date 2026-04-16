package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Session holds the runtime credentials for a logged-in user.
// Password is held in memory only — never written to disk.
type Session struct {
	ID       string
	User     string
	Password string
	Created  time.Time
}

// SessionStore is a concurrency-safe in-memory session map.
// Sessions are lost on server restart; users re-login (acceptable for an ops tool).
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*Session)}
}

// Create generates a new session for the given user and adds it to the store.
func (s *SessionStore) Create(user, password string) *Session {
	id := randomID()
	sess := &Session{
		ID:       id,
		User:     user,
		Password: password,
		Created:  time.Now(),
	}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	return sess
}

// Get looks up a session by ID. Returns a value copy and true if found.
func (s *SessionStore) Get(id string) (Session, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return Session{}, false
	}
	return *sess, true
}

// Delete removes a session by ID. No-op if the session doesn't exist.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

func randomID() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("auth: crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b)
}
