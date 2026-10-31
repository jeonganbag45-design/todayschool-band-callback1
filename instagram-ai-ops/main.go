package main

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

//go:embed web/*
var webFS embed.FS

const appVersion = "0.3.2"

type App struct {
	ig *IGClient
}

func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errOut(w http.ResponseWriter, status int, msg string) {
	jsonOut(w, status, map[string]any{"ok": false, "error": msg})
}

func baseURL(r *http.Request) string {
	if v := strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"); v != "" {
		return v
	}
	scheme := "http"
	if secureRequest(r) {
		scheme = "https"
	}
	host := r.Host
	if x := r.Header.Get("X-Forwarded-Host"); x != "" {
		host = strings.TrimSpace(strings.Split(x, ",")[0])
	}
	return scheme + "://" + host
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func setCookie(w http.ResponseWriter, r *http.Request, name, val string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: val, Path: "/", HttpOnly: true,
		Secure: secureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	})
}

func metaEnv() (string, string) {
	return strings.TrimSpace(os.Getenv("META_INSTAGRAM_APP_ID")), strings.TrimSpace(os.Getenv("META_INSTAGRAM_APP_SECRET"))
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(static))))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		appID, secret := metaEnv()
		jsonOut(w, 200, map[string]any{"ok": true, "version": appVersion, "instagram_configured": appID != "" && secret != ""})
	})
	mux.HandleFunc("/auth/instagram/start", a.authStart)
	mux.HandleFunc("/auth/instagram/callback", a.authCallback)
	mux.HandleFunc("/api/status", a.status)
	mux.HandleFunc("/api/accounts/resolve", a.resolveAccounts)
	mux.HandleFunc("/api/accounts/sync", a.syncAccount)
	mux.HandleFunc("/", a.index)
	return mux
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, _ := webFS.ReadFile("web/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func (a *App) status(w http.ResponseWriter, r *http.Request) {
	appID, secret := metaEnv()
	jsonOut(w, 200, map[string]any{
		"ok": true,
		"version": appVersion,
		"configured": appID != "" && secret != "",
		"redirect_uri": baseURL(r) + "/auth/instagram/callback",
		"storage": "연결 계정 정보는 이 브라우저에 암호화 저장",
	})
}

func decodeBlobs(r *http.Request) ([]string, error) {
	var in struct {
		Blobs []string `json:"blobs"`
	}
	err := json.NewDecoder(r.Body).Decode(&in)
	return in.Blobs, err
}

func (a *App) resolveAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errOut(w, 405, "POST만 지원합니다.")
		return
	}
	bs, err := decodeBlobs(r)
	if err != nil {
		errOut(w, 400, "저장된 계정 정보를 읽지 못했습니다.")
		return
	}
	accounts := make([]SafeAccount, 0, len(bs))
	valid := make([]string, 0, len(bs))
	for _, b := range bs {
		acc, err := openAccount(b)
		if err != nil {
			continue
		}
		accounts = append(accounts, safeAccount(acc))
		valid = append(valid, b)
	}
	jsonOut(w, 200, map[string]any{"ok": true, "accounts": accounts, "blobs": valid})
}

func accountFromProfile(p profileResp, media []Media, token string, expires int64) Account {
	id := p.UserID
	if id == "" {
		id = p.ID
	}
	if expires <= 0 {
		expires = 59 * 24 * 3600
	}
	return Account{
		ID: id, Username: p.Username, Name: p.Name, ProfilePictureURL: p.ProfilePictureURL,
		FollowersCount: p.FollowersCount, MediaCount: p.MediaCount, AccountType: p.AccountType,
		AccessToken: token, TokenExpiresAt: time.Now().Add(time.Duration(expires) * time.Second),
		LastSyncedAt: time.Now(), RecentMedia: media,
	}
}

func (a *App) syncAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errOut(w, 405, "POST만 지원합니다.")
		return
	}
	var in struct {
		Blob string `json:"blob"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || in.Blob == "" {
		errOut(w, 400, "계정 연결 정보가 없습니다.")
		return
	}
	acc, err := openAccount(in.Blob)
	if err != nil {
		errOut(w, 400, "저장된 계정 연결 정보를 읽지 못했습니다. 다시 연결하세요.")
		return
	}
	token := acc.AccessToken
	if !acc.TokenExpiresAt.IsZero() && time.Until(acc.TokenExpiresAt) < 14*24*time.Hour {
		if nt, sec, e := a.ig.RefreshToken(r.Context(), token); e == nil && nt != "" {
			token = nt
			acc.TokenExpiresAt = time.Now().Add(time.Duration(sec) * time.Second)
		}
	}
	p, media, err := a.ig.SyncAccount(r.Context(), token)
	if err != nil {
		errOut(w, 502, err.Error())
		return
	}
	seconds := int64(time.Until(acc.TokenExpiresAt).Seconds())
	if seconds <= 0 {
		seconds = 59 * 24 * 3600
	}
	fresh := accountFromProfile(p, media, token, seconds)
	if fresh.ID == "" {
		fresh.ID = acc.ID
	}
	blob, err := sealAccount(fresh)
	if err != nil {
		errOut(w, 500, "동기화 정보를 암호화하지 못했습니다.")
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true, "account": safeAccount(fresh), "blob": blob})
}

func (a *App) authStart(w http.ResponseWriter, r *http.Request) {
	appID, secret := metaEnv()
	if appID == "" || secret == "" {
		http.Redirect(w, r, "/?connect_error="+url.QueryEscape("Instagram 연결용 서버 설정이 아직 완료되지 않았습니다."), 302)
		return
	}
	state := randomString(24)
	redirect := baseURL(r) + "/auth/instagram/callback"
	setCookie(w, r, "igops_oauth_state", state, 600)
	setCookie(w, r, "igops_oauth_redirect", base64.RawURLEncoding.EncodeToString([]byte(redirect)), 600)
	http.Redirect(w, r, a.ig.AuthURL(appID, redirect, state), 302)
}

func (a *App) authCallback(w http.ResponseWriter, r *http.Request) {
	if er := r.URL.Query().Get("error"); er != "" {
		http.Redirect(w, r, "/?connect_error="+url.QueryEscape(r.URL.Query().Get("error_description")), 302)
		return
	}
	stateCookie, err := r.Cookie("igops_oauth_state")
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		http.Redirect(w, r, "/?connect_error="+url.QueryEscape("Instagram 인증 상태가 일치하지 않습니다. 다시 연결하세요."), 302)
		return
	}
	redirectCookie, err := r.Cookie("igops_oauth_redirect")
	if err != nil {
		http.Redirect(w, r, "/?connect_error="+url.QueryEscape("OAuth 콜백 주소 정보를 찾지 못했습니다."), 302)
		return
	}
	redirectBytes, err := base64.RawURLEncoding.DecodeString(redirectCookie.Value)
	if err != nil {
		http.Redirect(w, r, "/?connect_error="+url.QueryEscape("OAuth 콜백 주소를 읽지 못했습니다."), 302)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, "/?connect_error="+url.QueryEscape("Instagram 인증 코드를 받지 못했습니다."), 302)
		return
	}
	appID, secret := metaEnv()
	if appID == "" || secret == "" {
		http.Redirect(w, r, "/?connect_error="+url.QueryEscape("Instagram 연결용 서버 설정이 없습니다."), 302)
		return
	}
	token, uid, expires, err := a.ig.ExchangeCode(r.Context(), appID, secret, string(redirectBytes), code)
	if err != nil {
		http.Redirect(w, r, "/?connect_error="+url.QueryEscape(err.Error()), 302)
		return
	}
	p, media, err := a.ig.SyncAccount(r.Context(), token)
	if err != nil {
		http.Redirect(w, r, "/?connect_error="+url.QueryEscape(err.Error()), 302)
		return
	}
	acc := accountFromProfile(p, media, token, expires)
	if acc.ID == "" {
		acc.ID = uid
	}
	if acc.ID == "" || acc.Username == "" {
		http.Redirect(w, r, "/?connect_error="+url.QueryEscape("Instagram 계정 정보를 확인하지 못했습니다."), 302)
		return
	}
	blob, err := sealAccount(acc)
	if err != nil {
		http.Redirect(w, r, "/?connect_error="+url.QueryEscape("연결 정보 암호화에 실패했습니다."), 302)
		return
	}
	http.Redirect(w, r, "/?connected_blob="+url.QueryEscape(blob)+"&connected_id="+url.QueryEscape(acc.ID), 302)
}

func main() {
	app := &App{ig: NewIGClient()}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8765"
	}
	log.Printf("Instagram AI Ops v%s listening on :%s", appVersion, port)
	log.Fatal(http.ListenAndServe(":"+port, app.routes()))
}
