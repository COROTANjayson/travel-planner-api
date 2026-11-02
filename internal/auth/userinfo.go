package auth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var errProfileUnauthorized = errors.New("profile identity does not match the access token")
var errProfileUnavailable = errors.New("profile unavailable")

type cachedProfile struct {
	profile claims
	expires time.Time
}

func (a *Auth) userInfo(ctx context.Context, issuer, subject, authorization string) (*claims, error) {
	key := sha256.Sum256([]byte(authorization))
	a.profileMu.Lock()
	cached, ok := a.profiles[key]
	a.profileMu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return &cached.profile, nil
	}
	// The issuer is trusted only after CheckJWT has validated issuer/audience/signature.
	endpoint, err := url.JoinPath(issuer, "userinfo")
	if err != nil {
		return nil, errProfileUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errProfileUnavailable
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Accept", "application/json")
	response, err := a.profileClient.Do(req)
	if err != nil {
		return nil, errProfileUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return nil, errProfileUnauthorized
	}
	if response.StatusCode != http.StatusOK {
		return nil, errProfileUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return nil, errProfileUnavailable
	}
	var info struct {
		claims
		Subject string `json:"sub"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, errProfileUnavailable
	}
	if info.Subject != subject {
		return nil, errProfileUnauthorized
	}
	if len(info.Email) > 320 || len(info.Name) > 200 || strings.ContainsRune(info.Email+info.Name, 0) {
		return nil, errProfileUnavailable
	}
	a.profileMu.Lock()
	// shortcut: clear the bounded cache at 256 tokens; use LRU if cache churn becomes measurable.
	if len(a.profiles) >= 256 {
		clear(a.profiles)
	}
	a.profiles[key] = cachedProfile{profile: info.claims, expires: time.Now().Add(time.Minute)}
	a.profileMu.Unlock()
	return &info.claims, nil
}
