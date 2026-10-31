package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const graphVersion = "v26.0"

type IGClient struct {
	HTTP                         *http.Client
	AuthBase, APIBase, TokenBase string
}

func NewIGClient() *IGClient {
	return &IGClient{HTTP: &http.Client{Timeout: 25 * time.Second}, AuthBase: "https://www.instagram.com", APIBase: "https://graph.instagram.com", TokenBase: "https://api.instagram.com"}
}
func (c *IGClient) AuthURL(appID, redirectURI, state string) string {
	q := url.Values{}
	q.Set("client_id", appID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("state", state)
	q.Set("enable_fb_login", "0")
	q.Set("force_reauth", "true")
	q.Set("scope", "instagram_business_basic")
	return c.AuthBase + "/oauth/authorize?" + q.Encode()
}

type tokenItem struct {
	AccessToken string          `json:"access_token"`
	UserID      json.RawMessage `json:"user_id"`
	ExpiresIn   int64           `json:"expires_in"`
}
type tokenEnvelope struct {
	AccessToken string          `json:"access_token"`
	UserID      json.RawMessage `json:"user_id"`
	ExpiresIn   int64           `json:"expires_in"`
	Data        []tokenItem     `json:"data"`
}
type longTokenResp struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

func exactJSONID(raw json.RawMessage) string {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return ""
	}
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		var s string
		if json.Unmarshal(b, &s) == nil {
			return s
		}
	}
	return string(b)
}
func apiError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return fmt.Errorf("Instagram API 오류 (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
}
func (c *IGClient) ExchangeCode(ctx context.Context, appID, secret, redirectURI, code string) (string, string, int64, error) {
	form := url.Values{"client_id": {appID}, "client_secret": {secret}, "grant_type": {"authorization_code"}, "redirect_uri": {redirectURI}, "code": {code}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenBase+"/oauth/access_token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return "", "", 0, e
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", "", 0, apiError(resp)
	}
	var env tokenEnvelope
	if e = json.NewDecoder(resp.Body).Decode(&env); e != nil {
		return "", "", 0, e
	}
	st := tokenItem{AccessToken: env.AccessToken, UserID: env.UserID, ExpiresIn: env.ExpiresIn}
	if st.AccessToken == "" && len(env.Data) == 1 {
		st = env.Data[0]
	}
	if st.AccessToken == "" {
		return "", "", 0, fmt.Errorf("Instagram 인증 응답에서 access token을 찾지 못했습니다")
	}
	uid := exactJSONID(st.UserID)
	q := url.Values{"grant_type": {"ig_exchange_token"}, "client_secret": {secret}, "access_token": {st.AccessToken}}
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.APIBase+"/access_token?"+q.Encode(), nil)
	resp2, e := c.HTTP.Do(req2)
	if e != nil {
		return "", "", 0, e
	}
	defer resp2.Body.Close()
	if resp2.StatusCode/100 != 2 {
		return "", "", 0, apiError(resp2)
	}
	var lt longTokenResp
	if e = json.NewDecoder(resp2.Body).Decode(&lt); e != nil {
		return "", "", 0, e
	}
	if lt.ExpiresIn == 0 {
		lt.ExpiresIn = 60 * 24 * 3600
	}
	return lt.AccessToken, uid, lt.ExpiresIn, nil
}

type profileResp struct {
	ID                string `json:"id"`
	UserID            string `json:"user_id"`
	Username          string `json:"username"`
	Name              string `json:"name"`
	ProfilePictureURL string `json:"profile_picture_url"`
	FollowersCount    int64  `json:"followers_count"`
	MediaCount        int64  `json:"media_count"`
	AccountType       string `json:"account_type"`
}
type profileEnvelope struct {
	Data []profileResp `json:"data"`
}
type mediaResp struct {
	Data []Media `json:"data"`
}

func decodeProfile(resp *http.Response) (profileResp, error) {
	b, e := io.ReadAll(resp.Body)
	if e != nil {
		return profileResp{}, e
	}
	var p profileResp
	if json.Unmarshal(b, &p) == nil && (p.ID != "" || p.UserID != "" || p.Username != "") {
		return p, nil
	}
	var env profileEnvelope
	if e := json.Unmarshal(b, &env); e == nil && len(env.Data) == 1 {
		return env.Data[0], nil
	}
	return profileResp{}, fmt.Errorf("Instagram 프로필 응답 형식을 읽지 못했습니다")
}
func (c *IGClient) fetchProfile(ctx context.Context, token string) (profileResp, error) {
	sets := []string{"user_id,id,username,name,profile_picture_url,followers_count,media_count,account_type", "id,username,name,profile_picture_url,followers_count,media_count,account_type", "user_id,username"}
	var last error
	for _, fields := range sets {
		u := fmt.Sprintf("%s/%s/me?fields=%s&access_token=%s", c.APIBase, graphVersion, url.QueryEscape(fields), url.QueryEscape(token))
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, e := c.HTTP.Do(req)
		if e != nil {
			return profileResp{}, e
		}
		if resp.StatusCode/100 == 2 {
			p, e := decodeProfile(resp)
			resp.Body.Close()
			if e == nil {
				return p, nil
			}
			last = e
			continue
		}
		last = apiError(resp)
		resp.Body.Close()
	}
	return profileResp{}, last
}
func (c *IGClient) fetchMedia(ctx context.Context, token string) ([]Media, error) {
	sets := []string{"id,caption,media_type,media_url,thumbnail_url,permalink,timestamp,like_count,comments_count", "id,caption,media_type,media_url,thumbnail_url,permalink,timestamp"}
	var last error
	for _, mf := range sets {
		u := fmt.Sprintf("%s/%s/me/media?fields=%s&limit=18&access_token=%s", c.APIBase, graphVersion, url.QueryEscape(mf), url.QueryEscape(token))
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, e := c.HTTP.Do(req)
		if e != nil {
			return nil, e
		}
		if resp.StatusCode/100 == 2 {
			var m mediaResp
			e = json.NewDecoder(resp.Body).Decode(&m)
			resp.Body.Close()
			if e == nil {
				return m.Data, nil
			}
			last = e
			continue
		}
		last = apiError(resp)
		resp.Body.Close()
	}
	return nil, last
}
func (c *IGClient) SyncAccount(ctx context.Context, token string) (profileResp, []Media, error) {
	p, e := c.fetchProfile(ctx, token)
	if e != nil {
		return profileResp{}, nil, e
	}
	m, e := c.fetchMedia(ctx, token)
	if e != nil {
		return p, nil, nil
	}
	return p, m, nil
}
func (c *IGClient) RefreshToken(ctx context.Context, token string) (string, int64, error) {
	q := url.Values{"grant_type": {"ig_refresh_token"}, "access_token": {token}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.APIBase+"/refresh_access_token?"+q.Encode(), nil)
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return "", 0, e
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", 0, apiError(resp)
	}
	var lt longTokenResp
	if e = json.NewDecoder(resp.Body).Decode(&lt); e != nil {
		return "", 0, e
	}
	return lt.AccessToken, lt.ExpiresIn, nil
}
