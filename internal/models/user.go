// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package models

// UserInfo represents the authenticated user's profile from the API.
type UserInfo struct {
	ID            int    `json:"id"`
	Email         string `json:"email"`
	Username      string `json:"username"`
	FullName      string `json:"full_name"`
	AvatarURL     string `json:"avatar_url"`
	EmailVerified bool   `json:"email_verified"`
}
