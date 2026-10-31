package main

import "time"

type Media struct {
	ID            string `json:"id"`
	Caption       string `json:"caption,omitempty"`
	MediaType     string `json:"media_type,omitempty"`
	MediaURL      string `json:"media_url,omitempty"`
	ThumbnailURL  string `json:"thumbnail_url,omitempty"`
	Permalink     string `json:"permalink,omitempty"`
	Timestamp     string `json:"timestamp,omitempty"`
	LikeCount     int64  `json:"like_count,omitempty"`
	CommentsCount int64  `json:"comments_count,omitempty"`
}

type Account struct {
	ID                string    `json:"id"`
	Username          string    `json:"username"`
	Name              string    `json:"name,omitempty"`
	ProfilePictureURL string    `json:"profile_picture_url,omitempty"`
	FollowersCount    int64     `json:"followers_count,omitempty"`
	MediaCount        int64     `json:"media_count,omitempty"`
	AccountType       string    `json:"account_type,omitempty"`
	AccessToken       string    `json:"access_token"`
	TokenExpiresAt    time.Time `json:"token_expires_at"`
	LastSyncedAt      time.Time `json:"last_synced_at"`
	RecentMedia       []Media   `json:"recent_media,omitempty"`
}

type SafeAccount struct {
	ID                string    `json:"id"`
	Username          string    `json:"username"`
	Name              string    `json:"name,omitempty"`
	ProfilePictureURL string    `json:"profile_picture_url,omitempty"`
	FollowersCount    int64     `json:"followers_count,omitempty"`
	MediaCount        int64     `json:"media_count,omitempty"`
	AccountType       string    `json:"account_type,omitempty"`
	TokenExpiresAt    time.Time `json:"token_expires_at"`
	LastSyncedAt      time.Time `json:"last_synced_at"`
	RecentMedia       []Media   `json:"recent_media,omitempty"`
}

func safeAccount(a Account) SafeAccount {
	return SafeAccount{ID: a.ID, Username: a.Username, Name: a.Name, ProfilePictureURL: a.ProfilePictureURL, FollowersCount: a.FollowersCount, MediaCount: a.MediaCount, AccountType: a.AccountType, TokenExpiresAt: a.TokenExpiresAt, LastSyncedAt: a.LastSyncedAt, RecentMedia: a.RecentMedia}
}
