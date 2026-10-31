package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"os"
	"strings"
	"time"
)

type Auth struct {
	password string
	secret   []byte
}

func NewAuth() *Auth {
	pw := strings.TrimSpace(os.Getenv("APP_ACCESS_PASSWORD"))
	sec := strings.TrimSpace(os.Getenv("SESSION_SECRET"))
	if sec == "" && pw != "" {
		sum := sha256.Sum256([]byte("session:" + pw))
		sec = base64.RawURLEncoding.EncodeToString(sum[:])
	}
	return &Auth{password: pw, secret: []byte(sec)}
}
func (a *Auth) enabled() bool { return a.password != "" }
func (a *Auth) sign(v string) string {
	m := hmac.New(sha256.New, a.secret)
	m.Write([]byte(v))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func (a *Auth) token() string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)))
	return payload + "." + a.sign(payload)
}
func (a *Auth) valid(v string) bool {
	if !a.enabled() || len(a.secret) == 0 {
		return false
	}
	p := strings.Split(v, ".")
	if len(p) != 2 || subtle.ConstantTimeCompare([]byte(a.sign(p[0])), []byte(p[1])) != 1 {
		return false
	}
	b, e := base64.RawURLEncoding.DecodeString(p[0])
	if e != nil {
		return false
	}
	t, e := time.Parse(time.RFC3339, string(b))
	return e == nil && time.Now().Before(t)
}
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
func (a *Auth) set(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "igops_session", Value: a.token(), Path: "/", HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
}
func (a *Auth) clear(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "igops_session", Value: "", Path: "/", HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
func (a *Auth) allowed(r *http.Request) bool {
	if !a.enabled() {
		return false
	}
	c, e := r.Cookie("igops_session")
	return e == nil && a.valid(c.Value)
}
func (a *Auth) check(v string) bool {
	if !a.enabled() {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(v), []byte(a.password)) == 1
}
