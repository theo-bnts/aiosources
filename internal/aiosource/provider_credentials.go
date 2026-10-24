package aiosource

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/theo-bnts/aiosources/internal/model"
)

func (p *ProviderHTTP) validateExtendedKey(ctx context.Context, service, key string) error {
	switch service {
	case "realdebrid":
		var user struct {
			Type    string `json:"type"`
			Premium *int64 `json:"premium"`
		}
		if err := p.providerJSON(ctx, service, key, http.MethodGet, "user", nil, &user); err != nil {
			return err
		}
		if user.Type == "" || user.Premium == nil {
			return ErrInvalidResponse
		}
		if user.Type != "premium" || *user.Premium <= 0 {
			return newProviderError("subscription", "")
		}
	case "premiumize":
		var user struct {
			PremiumUntil *json.Number `json:"premium_until"`
			Customer     string       `json:"customer_id"`
		}
		if err := p.providerJSON(ctx, service, key, http.MethodGet, "account/info", nil, &user); err != nil {
			return err
		}
		if user.Customer == "" {
			return ErrInvalidResponse
		}
		if user.PremiumUntil == nil {
			return newProviderError("subscription", "")
		}
		until, err := user.PremiumUntil.Int64()
		if err != nil {
			return ErrInvalidResponse
		}
		if until <= time.Now().Unix() {
			return newProviderError("subscription", "")
		}
	case "debridlink":
		var user struct {
			Type *int   `json:"accountType"`
			Left *int64 `json:"premiumLeft"`
		}
		if err := p.providerJSON(ctx, service, key, http.MethodGet, "account/infos", nil, &user); err != nil {
			return err
		}
		if user.Type == nil || *user.Type < 0 || *user.Type > 2 || user.Left == nil {
			return ErrInvalidResponse
		}
		if *user.Type == 0 || (*user.Type == 1 && *user.Left <= 0) {
			return newProviderError("subscription", "")
		}
	case "deepbrid":
		var user struct {
			Type string `json:"type"`
		}
		if err := p.providerJSON(ctx, service, key, http.MethodGet, "user", nil, &user); err != nil {
			return err
		}
		if user.Type == "" {
			return ErrInvalidResponse
		}
		if user.Type != "premium" {
			return newProviderError("subscription", "")
		}
	default:
		return model.ErrInvalid
	}
	return nil
}
