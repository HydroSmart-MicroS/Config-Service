package health

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	statusIdle = 10 * time.Minute
	statusCookie = "__Secure-hs-status"
)

type statusAttempts struct {
	count int
	until time.Time
}

type statusSessions struct {
	origin string
	hash []byte
	now func() time.Time
	mu sync.Mutex
	deadlines map[string]time.Time
	attempts map[string]statusAttempts
	checking chan struct{}
}

func (d *Dashboard) RequireSession(origin, passwordHash string) error {
	if _, err := bcrypt.Cost([]byte(passwordHash)); err != nil {
		return errors.New("invalid status panel password hash")
	}
	d.sessions = &statusSessions{
		origin: origin, hash: []byte(passwordHash), now: time.Now,
		deadlines: make(map[string]time.Time), attempts: make(map[string]statusAttempts),
		checking: make(chan struct{}, 1),
	}
	return nil
}

func statusToken(r *http.Request) string {
	cookie, err := r.Cookie(statusCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func setStatusCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: statusCookie, Value: token, Path: "/status/", MaxAge: maxAge,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
}

func (s *statusSessions) remaining(r *http.Request, touch bool) time.Duration {
	token := statusToken(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	deadline := s.deadlines[token]
	if !deadline.After(now) {
		delete(s.deadlines, token)
		return 0
	}
	if touch {
		deadline = now.Add(statusIdle)
		s.deadlines[token] = deadline
	}
	return deadline.Sub(now)
}

func (s *statusSessions) allowAttempt(client string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, attempt := range s.attempts {
		if !attempt.until.After(now) {
			delete(s.attempts, key)
		}
	}
	attempt, exists := s.attempts[client]
	if !exists {
		if len(s.attempts) >= 1024 {
			return false
		}
		attempt.until = now.Add(time.Minute)
	}
	if attempt.count >= 5 {
		return false
	}
	attempt.count++
	s.attempts[client] = attempt
	return true
}

func (s *statusSessions) start(oldToken string) (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, deadline := range s.deadlines {
		if !deadline.After(now) || key == oldToken {
			delete(s.deadlines, key)
		}
	}
	if len(s.deadlines) >= 128 {
		return "", errors.New("too many panel sessions")
	}
	s.deadlines[token] = now.Add(statusIdle)
	return token, nil
}

func (s *statusSessions) handle(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path == "/status/login" && r.Method == http.MethodGet {
		if s.remaining(r, false) > 0 {
			http.Redirect(w, r, "/status/", http.StatusSeeOther)
		} else {
			message := ""
			switch {
			case r.URL.Query().Get("expired") == "1":
				message = "La sesión se cerró. Vuelve a ingresar."
			case r.URL.Query().Get("signedout") == "1":
				message = "Cerraste sesión en el panel."
			}
			renderStatusLogin(w, http.StatusOK, message)
		}
		return true
	}
	switch path {
	case "/status/login", "/status/activity", "/status/logout":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
			return true
		}
		if r.Header.Get("Origin") != s.origin {
			http.Error(w, "Origen no permitido", http.StatusForbidden)
			return true
		}
		switch path {
		case "/status/login":
			s.login(w, r)
		case "/status/activity":
			remaining := s.remaining(r, true)
			if remaining == 0 {
				http.Error(w, "Sesión vencida", http.StatusUnauthorized)
			} else {
				w.Header().Set("X-Status-Idle-Ms", strconv.FormatInt(remaining.Milliseconds(), 10))
				w.WriteHeader(http.StatusNoContent)
			}
		case "/status/logout":
			s.mu.Lock()
			delete(s.deadlines, statusToken(r))
			s.mu.Unlock()
			setStatusCookie(w, "", -1)
			http.Redirect(w, r, "/status/login?signedout=1", http.StatusSeeOther)
		}
		return true
	case "/status/dashboard.css":
		return false
	}
	remaining := s.remaining(r, false)
	if remaining > 0 {
		w.Header().Set("X-Status-Idle-Ms", strconv.FormatInt(remaining.Milliseconds(), 10))
		return false
	}
	switch path {
	case "/", "/status", "/status/":
		http.Redirect(w, r, "/status/login", http.StatusSeeOther)
	default:
		http.Error(w, "Sesión requerida", http.StatusUnauthorized)
	}
	return true
}

func (s *statusSessions) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		renderStatusLogin(w, http.StatusBadRequest, "Revisa los datos del formulario.")
		return
	}
	client := r.Header.Get("X-Hydrosmart-Status-Client")
	if !s.allowAttempt(client) {
		w.Header().Set("Retry-After", "60")
		renderStatusLogin(w, http.StatusTooManyRequests, "Demasiados intentos. Espera un minuto.")
		return
	}
	select {
	case s.checking <- struct{}{}:
		defer func(){ <-s.checking }()
	default:
		renderStatusLogin(w, http.StatusTooManyRequests, "Inténtalo nuevamente en unos segundos.")
		return
	}
	password := r.PostForm.Get("password")
	if r.PostForm.Get("username") != "monitor" || len(password) == 0 || len(password) > 72 || bcrypt.CompareHashAndPassword(s.hash, []byte(password)) != nil {
		renderStatusLogin(w, http.StatusUnauthorized, "El usuario o la contraseña son incorrectos.")
		return
	}
	token, err := s.start(statusToken(r))
	if err != nil {
		renderStatusLogin(w, http.StatusServiceUnavailable, "No se pudo iniciar la sesión. Inténtalo de nuevo.")
		return
	}
	setStatusCookie(w, token, 0)
	http.Redirect(w, r, "/status/", http.StatusSeeOther)
}
