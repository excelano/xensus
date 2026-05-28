package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	sessionCookieName = "xensus_session"
	loginCookieName   = "xensus_login"
	sessionMaxAge     = 8 * time.Hour
	loginMaxAge       = 5 * time.Minute
)

type sessionData struct {
	OID string `json:"oid"`
	UPN string `json:"upn"`
	TID string `json:"tid"`
	Exp int64  `json:"exp"`
}

type loginData struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	ReturnTo string `json:"r"`
	Exp      int64  `json:"exp"`
}

// SessionStore encrypts session and login-state payloads into cookies
// with AES-256-GCM. The same key handles both — login state is just a
// short-lived encrypted cookie that carries the CSRF state and OIDC
// nonce across the redirect to login.microsoftonline.com.
type SessionStore struct {
	aead         cipher.AEAD
	trustProxy   bool
	keyGenerated bool
}

// NewSessionStore returns a store using the given base64-encoded 32-byte
// key. If keyB64 is empty a fresh random key is generated and the
// keyGenerated flag is set so the caller can log a warning that sessions
// will not survive a restart.
func NewSessionStore(keyB64 string, trustProxy bool) (*SessionStore, error) {
	var key []byte
	generated := false
	if keyB64 == "" {
		key = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("generate session key: %w", err)
		}
		generated = true
	} else {
		k, err := base64.StdEncoding.DecodeString(keyB64)
		if err != nil {
			return nil, fmt.Errorf("XENSUS_SESSION_KEY is not valid base64: %w", err)
		}
		if len(k) != 32 {
			return nil, fmt.Errorf("XENSUS_SESSION_KEY must decode to exactly 32 bytes (got %d)", len(k))
		}
		key = k
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("init GCM: %w", err)
	}
	return &SessionStore{aead: aead, trustProxy: trustProxy, keyGenerated: generated}, nil
}

// KeyGenerated reports whether the store fell back to an ephemeral key
// because XENSUS_SESSION_KEY was empty. Sessions issued in that mode
// will not survive a server restart.
func (s *SessionStore) KeyGenerated() bool { return s.keyGenerated }

func (s *SessionStore) seal(v any) (string, error) {
	plaintext, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := s.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(ct), nil
}

func (s *SessionStore) open(blob string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(blob)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	ns := s.aead.NonceSize()
	if len(raw) < ns {
		return fmt.Errorf("ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	plaintext, err := s.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}
	return json.Unmarshal(plaintext, v)
}

func (s *SessionStore) isSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if s.trustProxy && r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return false
}

func (s *SessionStore) writeCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(maxAge.Seconds()),
	})
}

func (s *SessionStore) clearCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// WriteSession encrypts and sets the long-lived session cookie.
func (s *SessionStore) WriteSession(w http.ResponseWriter, r *http.Request, sess sessionData) error {
	blob, err := s.seal(sess)
	if err != nil {
		return err
	}
	s.writeCookie(w, r, sessionCookieName, blob, sessionMaxAge)
	return nil
}

// ReadSession returns nil, nil if no cookie is present. Returns nil and
// an error if the cookie exists but is malformed, decrypts to garbage,
// or has expired — callers should treat that as "no session" and not
// surface the error to the user.
func (s *SessionStore) ReadSession(r *http.Request) (*sessionData, error) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, nil
	}
	var sess sessionData
	if err := s.open(c.Value, &sess); err != nil {
		return nil, err
	}
	if time.Now().Unix() > sess.Exp {
		return nil, fmt.Errorf("session expired")
	}
	return &sess, nil
}

// ClearSession deletes the session cookie by setting MaxAge=-1.
func (s *SessionStore) ClearSession(w http.ResponseWriter, r *http.Request) {
	s.clearCookie(w, r, sessionCookieName)
}

// WriteLoginState stores the OIDC state/nonce/return-to into a separate
// short-lived encrypted cookie. The callback reads and clears it.
func (s *SessionStore) WriteLoginState(w http.ResponseWriter, r *http.Request, ls loginData) error {
	blob, err := s.seal(ls)
	if err != nil {
		return err
	}
	s.writeCookie(w, r, loginCookieName, blob, loginMaxAge)
	return nil
}

func (s *SessionStore) ReadLoginState(r *http.Request) (*loginData, error) {
	c, err := r.Cookie(loginCookieName)
	if err != nil {
		return nil, nil
	}
	var ls loginData
	if err := s.open(c.Value, &ls); err != nil {
		return nil, err
	}
	if time.Now().Unix() > ls.Exp {
		return nil, fmt.Errorf("login state expired")
	}
	return &ls, nil
}

func (s *SessionStore) ClearLoginState(w http.ResponseWriter, r *http.Request) {
	s.clearCookie(w, r, loginCookieName)
}
