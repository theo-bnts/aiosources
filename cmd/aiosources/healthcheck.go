package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Probe the listening service without opening or migrating its database.
func healthcheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	address := environment("BIND", "127.0.0.1:8080")
	address = strings.Replace(address, "0.0.0.0:", "127.0.0.1:", 1)
	address = strings.Replace(address, "[::]:", "[::1]:", 1)
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+address+"/healthz", nil)
	if err != nil {
		return errors.New("invalid healthcheck address")
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("service healthcheck failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New("service is not healthy")
	}
	return nil
}
