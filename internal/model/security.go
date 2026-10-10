package model

import "time"

type AdminSession struct {
	ID         string    `json:"id"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

type SecurityStatus struct {
	MFAEnabled             bool       `json:"mfa_enabled"`
	RecoveryCodesRemaining int        `json:"recovery_codes_remaining"`
	PendingExpiresAt       *time.Time `json:"pending_expires_at,omitempty"`
}
