package browser

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const defaultStaleAge = 6 * time.Hour

type Config struct {
	AgentDockHome    string
	ExecutablePath   string
	CDPURL           string
	ReuseExistingCDP bool
}

type ScreenshotPublisher func(context.Context, []byte, int) (map[string]any, error)

type Service struct {
	mu                sync.Mutex
	cfg               Config
	sessions          map[string]*session
	profiles          map[string]string
	closed            bool
	publishScreenshot ScreenshotPublisher
	now               func() time.Time
	discoverCDP       func(context.Context) ([]cdpCandidate, error)
}

func New(cfg Config, publishScreenshot ScreenshotPublisher) *Service {
	return &Service{
		cfg:               cfg,
		sessions:          make(map[string]*session),
		profiles:          make(map[string]string),
		publishScreenshot: publishScreenshot,
		now:               time.Now,
		discoverCDP:       discoverCDPEndpoints,
	}
}

func (s *Service) start(ctx context.Context, req StartRequest) (StartResult, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return StartResult{}, browserError(ErrActionFailed, "browser service is closed", "runtime", nil, nil)
	}
	if req.Timeout <= 0 {
		req.Timeout = 30 * time.Second
	}
	if req.Browser == "" {
		req.Browser = BrowserAuto
	}
	if req.URL == "" {
		req.URL = "about:blank"
	}
	if req.Viewport.Width <= 0 {
		req.Viewport.Width = 1280
	}
	if req.Viewport.Height <= 0 {
		req.Viewport.Height = 800
	}

	cdpURL, connectionMode, err := s.resolveCDPConnection(ctx, req)
	if err != nil {
		return StartResult{}, err
	}

	var (
		sess       *session
		profileID  string
		profileDir string
		temporary  bool
		reserved   bool
	)
	defer func() {
		if reserved {
			s.releaseProfile(profileID, profileDir, temporary)
		}
	}()

	if cdpURL != "" {
		if strings.TrimSpace(req.ProfileID) != "" {
			return StartResult{}, browserError(ErrActionInvalid, "profile_id cannot be used with an external CDP browser", "input", &ErrorDetails{Field: "profile_id"}, nil)
		}
		if len(req.Cookies) != 0 {
			return StartResult{}, browserError(ErrActionInvalid, "cookies cannot be injected into an external CDP browser", "input", &ErrorDetails{Field: "cookies"}, nil)
		}
		if len(req.LocalStorage) != 0 {
			return StartResult{}, browserError(ErrActionInvalid, "local_storage cannot be injected into an external CDP browser", "input", &ErrorDetails{Field: "local_storage"}, nil)
		}
		// Resolve the browser websocket ourselves with a direct HTTP client so
		// CDP discovery never inherits HTTP(S)_PROXY from the host process.
		wsURL, resolveErr := resolveCDPWebSocket(ctx, cdpURL, req.Timeout)
		if resolveErr != nil {
			return StartResult{}, browserError(ErrCDPFailed, "resolve external CDP browser websocket", "cdp", nil, resolveErr)
		}
		if connectionMode != "external_configured" {
			if err := validateToolCDPURL(wsURL); err != nil {
				return StartResult{}, browserError(ErrCDPFailed, "resolved CDP websocket left the loopback trust boundary", "cdp", nil, err)
			}
		}
		allocatorCtx, allocatorCancel := chromedp.NewRemoteAllocator(context.Background(), wsURL, chromedp.NoModifyURL)
		browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
		sess = &session{
			id:              newSessionID(),
			kind:            BrowserAuto,
			external:        true,
			ownedTargets:    make(map[target.ID]struct{}),
			createdAt:       s.now(),
			lastActivity:    s.now(),
			allocatorCtx:    allocatorCtx,
			allocatorCancel: allocatorCancel,
			browserCtx:      browserCtx,
			browserCancel:   browserCancel,
			pages:           make(map[target.ID]*pageState),
			pageContexts:    make(map[target.ID]*pageContext),
		}
	} else {
		executable, findErr := FindExecutable(s.cfg.ExecutablePath, req.Browser)
		if findErr != nil {
			return StartResult{}, findErr
		}
		profileID = normalizeProfileID(req.ProfileID)
		profileDir, temporary, err = s.reserveProfile(profileID)
		if err != nil {
			return StartResult{}, err
		}
		reserved = true
		if temporary {
			profileDir, err = os.MkdirTemp(profileDir, "session-")
			if err != nil {
				return StartResult{}, browserError(ErrLaunchFailed, "create temporary browser profile", "browser_launch", nil, err)
			}
		} else if err := os.MkdirAll(profileDir, 0o700); err != nil {
			return StartResult{}, browserError(ErrLaunchFailed, "create browser profile", "browser_launch", &ErrorDetails{ProfileID: profileID}, err)
		}

		allocatorOptions := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
		allocatorOptions = append(allocatorOptions,
			chromedp.ExecPath(executable.Path),
			chromedp.UserDataDir(profileDir),
			chromedp.WindowSize(req.Viewport.Width, req.Viewport.Height),
			chromedp.Flag("headless", req.Headless),
			chromedp.Flag("disable-features", "site-per-process,Translate,BlinkGenPropertyTrees,BackForwardCache"),
			chromedp.Flag("no-first-run", true),
			chromedp.Flag("no-default-browser-check", true),
		)
		allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
		browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
		sess = &session{
			id:               newSessionID(),
			kind:             executable.Kind,
			profileID:        profileID,
			profileDir:       profileDir,
			temporaryProfile: temporary,
			ownedTargets:     make(map[target.ID]struct{}),
			createdAt:        s.now(),
			lastActivity:     s.now(),
			allocatorCtx:     allocatorCtx,
			allocatorCancel:  allocatorCancel,
			browserCtx:       browserCtx,
			browserCancel:    browserCancel,
			pages:            make(map[target.ID]*pageState),
			pageContexts:     make(map[target.ID]*pageContext),
		}
	}
	chromedp.ListenBrowser(sess.browserCtx, func(ev any) { sess.recordTargetEvent(ev) })

	launchCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	launchDone := make(chan error, 1)
	go func() { launchDone <- chromedp.Run(sess.browserCtx) }()
	select {
	case err := <-launchDone:
		if err != nil {
			sess.stop()
			return StartResult{}, classifyLaunchError(err)
		}
	case <-launchCtx.Done():
		// chromedp.Run may still be initializing its context when the launch timeout fires.
		// Abort only through cancellation functions here; stop() inspects chromedp context
		// state and would race with that initialization.
		sess.abortLaunch()
		return StartResult{}, classifyLaunchError(launchCtx.Err())
	}

	if sess.external {
		chromedpCtx := chromedp.FromContext(sess.browserCtx)
		if chromedpCtx == nil || chromedpCtx.Target == nil || chromedpCtx.Target.TargetID == "" {
			sess.stop()
			return StartResult{}, browserError(ErrCDPFailed, "external CDP browser did not provide an AgentDock target", "cdp", nil, nil)
		}
		sess.mu.Lock()
		sess.ownedTargets[chromedpCtx.Target.TargetID] = struct{}{}
		sess.mu.Unlock()
	}

	if err := sess.refreshPages(); err != nil {
		sess.stop()
		return StartResult{}, browserError(ErrCDPFailed, "list initial browser pages", "cdp", nil, err)
	}
	pageID, err := sess.selectPage("")
	if err != nil {
		sess.stop()
		return StartResult{}, err
	}
	pageCtx, err := sess.ensurePageContext(launchCtx, pageID)
	if err != nil {
		sess.stop()
		return StartResult{}, classifyOperationError(err, "page_attach")
	}
	if err := runWithContext(launchCtx, pageCtx, enableCoreDomainsAction()); err != nil {
		sess.stop()
		return StartResult{}, classifyOperationError(err, "browser_start")
	}
	if err := applyCookies(launchCtx, pageCtx, req.Cookies); err != nil {
		sess.stop()
		return StartResult{}, classifyOperationError(err, "browser_start")
	}
	if err := initialNavigation(launchCtx, pageCtx, req.URL); err != nil {
		sess.stop()
		return StartResult{}, classifyOperationError(err, "browser_start")
	}
	if err := applyLocalStorage(launchCtx, pageCtx, req.URL, req.LocalStorage, req.ReloadAfterLocalStorage); err != nil {
		sess.stop()
		return StartResult{}, classifyOperationError(err, "browser_start")
	}
	if err := sess.refreshPages(); err != nil {
		sess.stop()
		return StartResult{}, browserError(ErrCDPFailed, "refresh browser pages", "cdp", nil, err)
	}
	var currentURL, currentTitle string
	if err := runWithContext(launchCtx, pageCtx, chromedp.Location(&currentURL), chromedp.Title(&currentTitle)); err != nil {
		sess.stop()
		return StartResult{}, classifyOperationError(err, "browser_start")
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		sess.stop()
		return StartResult{}, browserError(ErrActionFailed, "browser service is closed", "runtime", nil, nil)
	}
	s.sessions[sess.id] = sess
	if profileID != "" {
		s.profiles[profileID] = sess.id
	}
	s.mu.Unlock()
	reserved = false

	pages := sess.pageSummaries(pageID, currentURL, currentTitle)
	return StartResult{
		SessionID:      sess.id,
		PageID:         string(pageID),
		Pages:          pages,
		URL:            currentURL,
		Title:          currentTitle,
		ProfileID:      profileID,
		ConnectionMode: connectionMode,
	}, nil
}

func (s *Service) resolveCDPConnection(ctx context.Context, req StartRequest) (string, string, error) {
	if cdpURL := strings.TrimSpace(req.CDPURL); cdpURL != "" {
		if err := validateToolCDPURL(cdpURL); err != nil {
			return "", "", browserError(ErrActionInvalid, "invalid cdp_url", "input", &ErrorDetails{Field: "cdp_url"}, err)
		}
		return cdpURL, "external_explicit", nil
	}
	if cdpURL := strings.TrimSpace(s.cfg.CDPURL); cdpURL != "" {
		if err := validateCDPURL(cdpURL); err != nil {
			return "", "", browserError(ErrActionInvalid, "invalid configured CDP URL", "input", &ErrorDetails{Field: "cdp_url"}, err)
		}
		return cdpURL, "external_configured", nil
	}
	if !s.cfg.ReuseExistingCDP {
		return "", "owned", nil
	}
	candidates, err := s.discoverCDP(ctx)
	if err != nil {
		return "", "", browserError(ErrCDPFailed, "discover existing CDP browsers", "cdp_discovery", nil, err)
	}
	switch len(candidates) {
	case 0:
		return "", "owned", nil
	case 1:
		return candidates[0].URL, "external_discovered", nil
	default:
		return "", "", browserError(ErrCDPAmbiguous, "multiple existing CDP browsers were discovered; configure cdp_url explicitly", "cdp_discovery", &ErrorDetails{Count: len(candidates)}, nil)
	}
}
