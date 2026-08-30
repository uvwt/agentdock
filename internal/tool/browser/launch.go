package browser

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func normalizeProfileID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		safe := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if safe {
			b.WriteRune(r)
			lastDash = r == '-'
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	cleaned := strings.Trim(b.String(), ".-_-")
	if len(cleaned) > 80 {
		cleaned = cleaned[:80]
	}
	if cleaned == "" {
		cleaned = "profile"
	}
	return cleaned
}

func newSessionID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "browser-" + hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("browser-%d", time.Now().UnixNano())
}

func classifyLaunchError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return browserError(ErrTimeout, "timed out launching browser", "browser_launch", nil, err)
	}
	return browserError(ErrLaunchFailed, "failed to launch browser", "browser_launch", nil, err)
}

func classifyOperationError(err error, phase string) error {
	if err == nil {
		return nil
	}
	var browserErr *Error
	if errors.As(err, &browserErr) {
		return browserErr
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return browserError(ErrTimeout, "browser operation timed out", phase, nil, err)
	}
	return browserError(ErrCDPFailed, "browser CDP operation failed", phase, nil, err)
}

func enableCoreDomainsAction() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if err := network.Enable().Do(ctx); err != nil {
			return err
		}
		return nil
	})
}

func applyCookies(parent, pageCtx context.Context, cookies []Cookie) error {
	if len(cookies) == 0 {
		return nil
	}
	actions := make([]chromedp.Action, 0, len(cookies)+1)
	actions = append(actions, network.Enable())
	for _, cookie := range cookies {
		cookie := cookie
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
			params := network.SetCookie(cookie.Name, cookie.Value)
			if cookie.URL != "" {
				params = params.WithURL(cookie.URL)
			}
			if cookie.Domain != "" {
				params = params.WithDomain(cookie.Domain)
			}
			if cookie.Path != "" {
				params = params.WithPath(cookie.Path)
			}
			params = params.WithHTTPOnly(cookie.HTTPOnly).WithSecure(cookie.Secure)
			switch strings.ToLower(cookie.SameSite) {
			case "strict":
				params = params.WithSameSite(network.CookieSameSiteStrict)
			case "lax":
				params = params.WithSameSite(network.CookieSameSiteLax)
			case "none":
				params = params.WithSameSite(network.CookieSameSiteNone)
			}
			if cookie.Expires > 0 {
				t := cdp.TimeSinceEpoch(time.Unix(int64(cookie.Expires), 0))
				params = params.WithExpires(&t)
			}
			return params.Do(ctx)
		}))
	}
	return runWithContext(parent, pageCtx, actions...)
}

func initialNavigation(parent, pageCtx context.Context, url string) error {
	if strings.TrimSpace(url) == "" || url == "about:blank" {
		return nil
	}
	return navigateAndWait(parent, pageCtx, WaitLoad, 0, func(ctx context.Context) error {
		_, _, _, _, err := page.Navigate(url).Do(ctx)
		return err
	})
}

func applyLocalStorage(parent, pageCtx context.Context, finalURL string, values map[string]map[string]string, reload bool) error {
	if len(values) == 0 {
		return nil
	}
	origins := make([]string, 0, len(values))
	for origin := range values {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	for _, origin := range origins {
		if err := initialNavigation(parent, pageCtx, origin); err != nil {
			return err
		}
		entries := values[origin]
		for key, value := range entries {
			expr := fmt.Sprintf("localStorage.setItem(%q,%q)", key, value)
			if err := runWithContext(parent, pageCtx, chromedp.Evaluate(expr, nil)); err != nil {
				return err
			}
		}
	}
	if finalURL == "about:blank" {
		// localStorage 注入会临时访问各 origin；默认起始页仍必须恢复为契约规定的 about:blank。
		if err := runWithContext(parent, pageCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, _, _, err := page.Navigate(finalURL).Do(ctx)
			return err
		})); err != nil {
			return err
		}
	} else if finalURL != "" {
		if err := initialNavigation(parent, pageCtx, finalURL); err != nil {
			return err
		}
	}
	if reload && finalURL != "" && finalURL != "about:blank" {
		return navigateAndWait(parent, pageCtx, WaitLoad, 0, func(ctx context.Context) error { return chromedp.Reload().Do(ctx) })
	}
	return nil
}

func runWithContext(parent, pageCtx context.Context, actions ...chromedp.Action) error {
	ctx, cancel := context.WithCancel(pageCtx)
	defer cancel()
	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return chromedp.Run(ctx, actions...)
}
