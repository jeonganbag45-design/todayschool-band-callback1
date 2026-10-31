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

const appVersion = "0.3.1"

type App struct {
	ig   *IGClient
	auth *Auth
}

func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func errOut(w http.ResponseWriter, status int, msg string) { jsonOut(w, status, map[string]any{"ok": false, "error": msg}) }
func baseURL(r *http.Request) string {
	if v := strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"); v != "" { return v }
	scheme := "http"; if secureRequest(r) { scheme = "https" }
	host := r.Host; if x := r.Header.Get("X-Forwarded-Host"); x != "" { host = strings.TrimSpace(strings.Split(x, ",")[0]) }
	return scheme + "://" + host
}
func randomString(n int) string { b:=make([]byte,n); _,_=rand.Read(b); return base64.RawURLEncoding.EncodeToString(b) }
func setCookie(w http.ResponseWriter,r *http.Request,name,val string,maxAge int){ http.SetCookie(w,&http.Cookie{Name:name,Value:val,Path:"/",HttpOnly:true,Secure:secureRequest(r),SameSite:http.SameSiteLaxMode,MaxAge:maxAge}) }

func (a *App) routes() http.Handler {
	mux:=http.NewServeMux(); static,_:=fs.Sub(webFS,"web")
	mux.Handle("/assets/",http.StripPrefix("/assets/",http.FileServer(http.FS(static))))
	mux.HandleFunc("/health",func(w http.ResponseWriter,r *http.Request){jsonOut(w,200,map[string]any{"ok":true,"version":appVersion,"setup_required":!a.auth.enabled()})})
	mux.HandleFunc("/login",a.login); mux.HandleFunc("/logout",a.logout)
	mux.HandleFunc("/auth/instagram/start",a.authStart); mux.HandleFunc("/auth/instagram/callback",a.authCallback)
	mux.HandleFunc("/api/status",a.status); mux.HandleFunc("/api/meta-config",a.metaConfigHandler)
	mux.HandleFunc("/api/accounts/resolve",a.resolveAccounts); mux.HandleFunc("/api/accounts/token",a.connectToken); mux.HandleFunc("/api/accounts/sync",a.syncAccount)
	mux.HandleFunc("/",a.index)
	return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		public:=r.URL.Path=="/health"||r.URL.Path=="/login"||r.URL.Path=="/auth/instagram/callback"||strings.HasPrefix(r.URL.Path,"/assets/")
		if public { mux.ServeHTTP(w,r); return }
		if !a.auth.allowed(r) { if strings.HasPrefix(r.URL.Path,"/api/"){errOut(w,401,"로그인이 필요합니다.");return}; http.Redirect(w,r,"/login",302);return }
		mux.ServeHTTP(w,r)
	})
}
func (a *App) index(w http.ResponseWriter,r *http.Request){ if r.URL.Path!="/"{http.NotFound(w,r);return}; b,_:=webFS.ReadFile("web/index.html"); w.Header().Set("Content-Type","text/html; charset=utf-8"); _,_=w.Write(b) }
func (a *App) login(w http.ResponseWriter,r *http.Request){
	if !a.auth.enabled(){w.Header().Set("Content-Type","text/html; charset=utf-8");w.WriteHeader(503);_,_=w.Write([]byte(`<!doctype html><html lang="ko"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>설정 필요</title><body style="font-family:system-ui;background:#f6f7fb;padding:40px"><h1>앱 비밀번호 설정이 필요합니다</h1><p>Render Environment에 APP_ACCESS_PASSWORD를 추가하세요.</p></body></html>`));return}
	if r.Method==http.MethodGet{if a.auth.allowed(r){http.Redirect(w,r,"/",302);return};b,_:=webFS.ReadFile("web/login.html");w.Header().Set("Content-Type","text/html; charset=utf-8");_,_=w.Write(b);return}
	if r.Method!=http.MethodPost{http.Error(w,"Method Not Allowed",405);return};_=r.ParseForm();if !a.auth.check(r.FormValue("password")){http.Redirect(w,r,"/login?error=1",302);return};a.auth.set(w,r);http.Redirect(w,r,"/",302)
}
func (a *App) logout(w http.ResponseWriter,r *http.Request){a.auth.clear(w,r);http.Redirect(w,r,"/login",302)}
func (a *App) status(w http.ResponseWriter,r *http.Request){cfg:=a.metaConfig(r);jsonOut(w,200,map[string]any{"ok":true,"version":appVersion,"configured":cfg.valid(),"app_id":cfg.AppID,"redirect_uri":baseURL(r)+"/auth/instagram/callback","storage":"계정 토큰과 Meta Secret은 암호화 저장"})}
func decodeBlobs(r *http.Request)([]string,error){var in struct{Blobs []string `json:"blobs"`};e:=json.NewDecoder(r.Body).Decode(&in);return in.Blobs,e}
func (a *App) resolveAccounts(w http.ResponseWriter,r *http.Request){if r.Method!=http.MethodPost{errOut(w,405,"POST만 지원합니다.");return};bs,e:=decodeBlobs(r);if e!=nil{errOut(w,400,"저장된 계정 정보를 읽지 못했습니다.");return};accounts:=make([]SafeAccount,0,len(bs));valid:=make([]string,0,len(bs));for _,b:=range bs{acc,e:=openAccount(b);if e!=nil{continue};accounts=append(accounts,safeAccount(acc));valid=append(valid,b)};jsonOut(w,200,map[string]any{"ok":true,"accounts":accounts,"blobs":valid})}
func accountFromProfile(p profileResp,media []Media,token string,expires int64)Account{id:=p.UserID;if id==""{id=p.ID};if expires<=0{expires=59*24*3600};return Account{ID:id,Username:p.Username,Name:p.Name,ProfilePictureURL:p.ProfilePictureURL,FollowersCount:p.FollowersCount,MediaCount:p.MediaCount,AccountType:p.AccountType,AccessToken:token,TokenExpiresAt:time.Now().Add(time.Duration(expires)*time.Second),LastSyncedAt:time.Now(),RecentMedia:media}}
func (a *App) connectToken(w http.ResponseWriter,r *http.Request){if r.Method!=http.MethodPost{errOut(w,405,"POST만 지원합니다.");return};var in struct{AccessToken string `json:"access_token"`};if json.NewDecoder(r.Body).Decode(&in)!=nil||strings.TrimSpace(in.AccessToken)==""{errOut(w,400,"Instagram Access Token을 입력하세요.");return};token:=strings.TrimSpace(in.AccessToken);p,m,e:=a.ig.SyncAccount(r.Context(),token);if e!=nil{errOut(w,502,e.Error());return};acc:=accountFromProfile(p,m,token,59*24*3600);if acc.ID==""||acc.Username==""{errOut(w,502,"Instagram 계정 정보를 확인하지 못했습니다.");return};blob,e:=sealAccount(acc);if e!=nil{errOut(w,500,"계정 연결 정보 암호화에 실패했습니다.");return};jsonOut(w,200,map[string]any{"ok":true,"account":safeAccount(acc),"blob":blob})}
func (a *App) syncAccount(w http.ResponseWriter,r *http.Request){if r.Method!=http.MethodPost{errOut(w,405,"POST만 지원합니다.");return};var in struct{Blob string `json:"blob"`};if json.NewDecoder(r.Body).Decode(&in)!=nil||in.Blob==""{errOut(w,400,"계정 연결 정보가 없습니다.");return};acc,e:=openAccount(in.Blob);if e!=nil{errOut(w,400,"저장된 계정 연결 정보를 읽지 못했습니다.");return};token:=acc.AccessToken;if !acc.TokenExpiresAt.IsZero()&&time.Until(acc.TokenExpiresAt)<14*24*time.Hour{if nt,sec,x:=a.ig.RefreshToken(r.Context(),token);x==nil&&nt!=""{token=nt;acc.TokenExpiresAt=time.Now().Add(time.Duration(sec)*time.Second)}};p,m,e:=a.ig.SyncAccount(r.Context(),token);if e!=nil{errOut(w,502,e.Error());return};seconds:=int64(time.Until(acc.TokenExpiresAt).Seconds());if seconds<=0{seconds=59*24*3600};fresh:=accountFromProfile(p,m,token,seconds);if fresh.ID==""{fresh.ID=acc.ID};blob,e:=sealAccount(fresh);if e!=nil{errOut(w,500,"동기화 정보를 암호화하지 못했습니다.");return};jsonOut(w,200,map[string]any{"ok":true,"account":safeAccount(fresh),"blob":blob})}
func (a *App) authStart(w http.ResponseWriter,r *http.Request){cfg:=a.metaConfig(r);if !cfg.valid(){http.Redirect(w,r,"/?connect_error="+url.QueryEscape("먼저 앱 안에서 Meta App ID와 App Secret을 저장하세요."),302);return};state:=randomString(24);redirect:=baseURL(r)+"/auth/instagram/callback";setCookie(w,r,"igops_oauth_state",state,600);setCookie(w,r,"igops_oauth_redirect",base64.RawURLEncoding.EncodeToString([]byte(redirect)),600);http.Redirect(w,r,a.ig.AuthURL(cfg.AppID,redirect,state),302)}
func (a *App) authCallback(w http.ResponseWriter,r *http.Request){if er:=r.URL.Query().Get("error");er!=""{http.Redirect(w,r,"/?connect_error="+url.QueryEscape(r.URL.Query().Get("error_description")),302);return};sc,e:=r.Cookie("igops_oauth_state");if e!=nil||sc.Value==""||sc.Value!=r.URL.Query().Get("state"){http.Redirect(w,r,"/?connect_error="+url.QueryEscape("Instagram 인증 상태가 일치하지 않습니다. 다시 연결하세요."),302);return};rc,e:=r.Cookie("igops_oauth_redirect");if e!=nil{http.Redirect(w,r,"/?connect_error="+url.QueryEscape("OAuth 콜백 주소 정보를 찾지 못했습니다."),302);return};rb,e:=base64.RawURLEncoding.DecodeString(rc.Value);if e!=nil{http.Redirect(w,r,"/?connect_error="+url.QueryEscape("OAuth 콜백 주소를 읽지 못했습니다."),302);return};code:=r.URL.Query().Get("code");if code==""{http.Redirect(w,r,"/?connect_error="+url.QueryEscape("Instagram 인증 코드를 받지 못했습니다."),302);return};cfg:=a.metaConfig(r);if !cfg.valid(){http.Redirect(w,r,"/?connect_error="+url.QueryEscape("저장된 Meta App 설정을 찾지 못했습니다."),302);return};token,uid,expires,e:=a.ig.ExchangeCode(r.Context(),cfg.AppID,cfg.AppSecret,string(rb),code);if e!=nil{http.Redirect(w,r,"/?connect_error="+url.QueryEscape(e.Error()),302);return};p,m,e:=a.ig.SyncAccount(r.Context(),token);if e!=nil{http.Redirect(w,r,"/?connect_error="+url.QueryEscape(e.Error()),302);return};acc:=accountFromProfile(p,m,token,expires);if acc.ID==""{acc.ID=uid};if acc.ID==""||acc.Username==""{http.Redirect(w,r,"/?connect_error="+url.QueryEscape("Instagram 계정 정보를 확인하지 못했습니다."),302);return};blob,e:=sealAccount(acc);if e!=nil{http.Redirect(w,r,"/?connect_error="+url.QueryEscape("연결 정보 암호화에 실패했습니다."),302);return};http.Redirect(w,r,"/?connected_blob="+url.QueryEscape(blob)+"&connected_id="+url.QueryEscape(acc.ID),302)}
func main(){app:=&App{ig:NewIGClient(),auth:NewAuth()};port:=os.Getenv("PORT");if port==""{port="8765"};log.Printf("Instagram AI Ops v%s listening on :%s",appVersion,port);log.Fatal(http.ListenAndServe(":"+port,app.routes()))}
