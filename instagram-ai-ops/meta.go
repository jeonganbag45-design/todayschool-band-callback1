package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

const metaCookieName = "igops_meta_config"

type MetaConfig struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

func (m MetaConfig) valid() bool {
	return strings.TrimSpace(m.AppID) != "" && strings.TrimSpace(m.AppSecret) != ""
}

func (a *App) metaConfig(r *http.Request) MetaConfig {
	env := MetaConfig{AppID: strings.TrimSpace(os.Getenv("META_INSTAGRAM_APP_ID")), AppSecret: strings.TrimSpace(os.Getenv("META_INSTAGRAM_APP_SECRET"))}
	if env.valid() { return env }
	c, err := r.Cookie(metaCookieName)
	if err != nil || c.Value == "" { return MetaConfig{} }
	b, err := openBytes(c.Value)
	if err != nil { return MetaConfig{} }
	var cfg MetaConfig
	if json.Unmarshal(b, &cfg) != nil { return MetaConfig{} }
	cfg.AppID = strings.TrimSpace(cfg.AppID); cfg.AppSecret = strings.TrimSpace(cfg.AppSecret)
	return cfg
}

func (a *App) metaConfigHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := a.metaConfig(r)
		jsonOut(w, 200, map[string]any{"ok":true,"configured":cfg.valid(),"app_id":cfg.AppID,"redirect_uri":baseURL(r)+"/auth/instagram/callback"})
	case http.MethodPost:
		var cfg MetaConfig
		if json.NewDecoder(r.Body).Decode(&cfg) != nil { errOut(w,400,"Meta App 정보를 읽지 못했습니다."); return }
		cfg.AppID = strings.TrimSpace(cfg.AppID); cfg.AppSecret = strings.TrimSpace(cfg.AppSecret)
		if !cfg.valid() { errOut(w,400,"Meta App ID와 App Secret을 모두 입력하세요."); return }
		b,_ := json.Marshal(cfg); sealed,err := sealBytes(b)
		if err != nil { errOut(w,500,"Meta App 정보를 안전하게 저장하지 못했습니다."); return }
		setCookie(w,r,metaCookieName,sealed,30*24*3600)
		jsonOut(w,200,map[string]any{"ok":true,"configured":true,"app_id":cfg.AppID,"redirect_uri":baseURL(r)+"/auth/instagram/callback"})
	case http.MethodDelete:
		setCookie(w,r,metaCookieName,"",-1); jsonOut(w,200,map[string]any{"ok":true})
	default:
		errOut(w,405,"지원하지 않는 요청입니다.")
	}
}
