package browser

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
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

type Service struct {
	mu          sync.Mutex
	cfg         Config
	sessions    map[string]*session
	profiles    map[string]string
	closed      bool
	now         func() time.Time
	discoverCDP func(context.Context) ([]cdpCandidate, error)
}

func New(cfg Config) *Service {
	return &Service{
		cfg:         cfg,
		sessions:    make(map[string]*session),
		profiles:    make(map[string]string),
		now:         time.Now,
		discoverCDP: discoverCDPEndpoints,
	}
}

func (s *Service) Start(ctx context.Context, req StartRequest) (StartResult, error) {
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

func (s *Service) CloseSession(req CloseRequest) (CloseResult, error) {
	sess, err := s.removeSession(req.SessionID)
	if err != nil {
		return CloseResult{}, err
	}
	sess.stop()
	s.releaseSessionProfile(sess)
	return CloseResult{SessionID: req.SessionID, Closed: true}, nil
}

func (s *Service) CleanupStale(req CleanupRequest) CleanupResult {
	maxAge := req.MaxAge
	if maxAge <= 0 {
		maxAge = defaultStaleAge
	}
	cutoff := s.now().Add(-maxAge)

	s.mu.Lock()
	var stale []*session
	for id, sess := range s.sessions {
		sess.mu.Lock()
		lastActivity := sess.lastActivity
		sess.mu.Unlock()
		if lastActivity.After(cutoff) {
			continue
		}
		delete(s.sessions, id)
		stale = append(stale, sess)
	}
	s.mu.Unlock()

	removed := make([]string, 0, len(stale))
	for _, sess := range stale {
		removed = append(removed, sess.id)
		sess.stop()
		s.releaseSessionProfile(sess)
	}
	sort.Strings(removed)
	return CleanupResult{RemovedCount: len(removed), RemovedSessions: removed}
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	sessions := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.sessions = make(map[string]*session)
	s.mu.Unlock()

	for _, sess := range sessions {
		sess.stop()
		s.releaseSessionProfile(sess)
	}
	s.mu.Lock()
	s.profiles = make(map[string]string)
	s.mu.Unlock()
	return nil
}

func (s *Service) getSession(id string) (*session, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.sessions[id]; sess != nil {
		return sess, nil
	}
	return nil, browserError(ErrSessionNotFound, "browser session was not found", "session", &ErrorDetails{SessionID: id}, nil)
}

func (s *Service) removeSession(id string) (*session, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[id]
	if sess == nil {
		return nil, browserError(ErrSessionNotFound, "browser session was not found", "session", &ErrorDetails{SessionID: id}, nil)
	}
	delete(s.sessions, id)
	return sess, nil
}

func (s *Service) releaseSessionProfile(sess *session) {
	if sess == nil || sess.profileID == "" {
		return
	}
	s.mu.Lock()
	if s.profiles[sess.profileID] == sess.id {
		delete(s.profiles, sess.profileID)
	}
	s.mu.Unlock()
}

func (s *Service) reserveProfile(profileID string) (string, bool, error) {
	root := filepath.Join(s.cfg.AgentDockHome, "browser")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", false, browserError(ErrLaunchFailed, "create browser state directory", "browser_launch", nil, err)
	}
	if profileID == "" {
		tempRoot := filepath.Join(root, "tmp")
		if err := os.MkdirAll(tempRoot, 0o700); err != nil {
			return "", false, browserError(ErrLaunchFailed, "create temporary browser directory", "browser_launch", nil, err)
		}
		return tempRoot, true, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sessionID := s.profiles[profileID]; sessionID != "" {
		return "", false, browserError(ErrProfileInUse, "browser profile is already in use", "profile", &ErrorDetails{ProfileID: profileID, SessionID: sessionID}, nil)
	}
	// 先占位，避免两个并发 start 同时穿过 profile 检查。
	s.profiles[profileID] = "<starting>"
	return filepath.Join(root, "profiles", profileID), false, nil
}

func (s *Service) releaseProfile(profileID, profileDir string, temporary bool) {
	if profileID != "" {
		s.mu.Lock()
		if s.profiles[profileID] == "<starting>" {
			delete(s.profiles, profileID)
		}
		s.mu.Unlock()
	}
	if temporary && strings.Contains(profileDir, filepath.Join("browser", "tmp")) {
		_ = os.RemoveAll(profileDir)
	}
}

func (sess *session) abortLaunch() {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.closed = true
	browserCancel := sess.browserCancel
	allocatorCancel := sess.allocatorCancel
	sess.mu.Unlock()

	if browserCancel != nil {
		browserCancel()
	}
	if allocatorCancel != nil {
		allocatorCancel()
	}
}

func (sess *session) stop() {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	sess.closed = true
	browserCtx := sess.browserCtx
	browserCancel := sess.browserCancel
	allocatorCancel := sess.allocatorCancel
	profileDir := sess.profileDir
	temporary := sess.temporaryProfile
	external := sess.external
	pageCancels := make([]context.CancelFunc, 0, len(sess.pageContexts))
	for _, pageCtx := range sess.pageContexts {
		if pageCtx != nil && pageCtx.cancel != nil {
			pageCancels = append(pageCancels, pageCtx.cancel)
		}
	}
	sess.pageContexts = nil
	sess.mu.Unlock()

	if external {
		// RemoteAllocator never owns the external browser process. Cancel only the
		// AgentDock-created target contexts and disconnect the CDP client.
		for _, cancel := range pageCancels {
			cancel()
		}
		if browserCancel != nil {
			browserCancel()
		}
		if allocatorCancel != nil {
			allocatorCancel()
		}
		return
	}

	graceful := false
	if browserCtx != nil {
		if chromedpCtx := chromedp.FromContext(browserCtx); chromedpCtx != nil && chromedpCtx.Browser != nil {
			closeCtx, closeCancel := context.WithTimeout(browserCtx, 5*time.Second)
			if err := chromedp.Cancel(closeCtx); err == nil {
				graceful = true
			}
			closeCancel()
		}
	}
	if !graceful && browserCancel != nil {
		browserCancel()
	}
	for _, cancel := range pageCancels {
		cancel()
	}
	if allocatorCancel != nil {
		allocatorCancel()
	}
	if temporary {
		_ = os.RemoveAll(profileDir)
	}
}

func (sess *session) touch(now time.Time) {
	sess.mu.Lock()
	sess.lastActivity = now
	sess.mu.Unlock()
}

func (sess *session) recordTargetEvent(ev any) {
	var pageCancel context.CancelFunc
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return
	}
	switch event := ev.(type) {
	case *target.EventTargetCreated:
		if event.TargetInfo == nil || event.TargetInfo.Type != "page" || !sess.allowTargetLocked(event.TargetInfo, true) {
			sess.mu.Unlock()
			return
		}
		sess.pageOrder++
		sess.pages[event.TargetInfo.TargetID] = &pageState{ID: event.TargetInfo.TargetID, URL: event.TargetInfo.URL, Title: event.TargetInfo.Title, Order: sess.pageOrder}
		sess.activePage = event.TargetInfo.TargetID
	case *target.EventTargetInfoChanged:
		if event.TargetInfo == nil || event.TargetInfo.Type != "page" || !sess.allowTargetLocked(event.TargetInfo, true) {
			sess.mu.Unlock()
			return
		}
		page := sess.pages[event.TargetInfo.TargetID]
		if page == nil {
			sess.pageOrder++
			page = &pageState{ID: event.TargetInfo.TargetID, Order: sess.pageOrder}
			sess.pages[event.TargetInfo.TargetID] = page
		}
		page.URL = event.TargetInfo.URL
		page.Title = event.TargetInfo.Title
	case *target.EventTargetDestroyed:
		delete(sess.pages, event.TargetID)
		delete(sess.ownedTargets, event.TargetID)
		if pageCtx := sess.pageContexts[event.TargetID]; pageCtx != nil {
			pageCancel = pageCtx.cancel
			delete(sess.pageContexts, event.TargetID)
		}
		if sess.activePage == event.TargetID {
			sess.activePage = sess.mostRecentPageLocked()
		}
	}
	sess.mu.Unlock()
	if pageCancel != nil {
		go pageCancel()
	}
}

func (sess *session) allowTargetLocked(info *target.Info, adoptChild bool) bool {
	if !sess.external {
		return true
	}
	if info == nil {
		return false
	}
	if _, ok := sess.ownedTargets[info.TargetID]; ok {
		return true
	}
	if adoptChild && info.OpenerID != "" {
		if _, ok := sess.ownedTargets[info.OpenerID]; ok {
			sess.ownedTargets[info.TargetID] = struct{}{}
			return true
		}
	}
	return false
}

func (sess *session) refreshPages() error {
	targets, err := chromedp.Targets(sess.browserCtx)
	if err != nil {
		return err
	}
	sess.mu.Lock()
	alive := make(map[target.ID]struct{})
	remaining := append([]*target.Info(nil), targets...)
	for len(remaining) > 0 {
		progress := false
		next := remaining[:0]
		for _, info := range remaining {
			if info == nil || info.Type != "page" {
				continue
			}
			if !sess.allowTargetLocked(info, true) {
				next = append(next, info)
				continue
			}
			progress = true
			alive[info.TargetID] = struct{}{}
			page := sess.pages[info.TargetID]
			if page == nil {
				sess.pageOrder++
				page = &pageState{ID: info.TargetID, Order: sess.pageOrder}
				sess.pages[info.TargetID] = page
				sess.activePage = info.TargetID
			}
			page.URL = info.URL
			page.Title = info.Title
		}
		if !progress || len(next) == 0 {
			break
		}
		remaining = next
	}
	var staleCancels []context.CancelFunc
	for id := range sess.pages {
		if _, ok := alive[id]; ok {
			continue
		}
		delete(sess.pages, id)
		delete(sess.ownedTargets, id)
		if pageCtx := sess.pageContexts[id]; pageCtx != nil {
			staleCancels = append(staleCancels, pageCtx.cancel)
			delete(sess.pageContexts, id)
		}
	}
	if _, ok := sess.pages[sess.activePage]; !ok {
		sess.activePage = sess.mostRecentPageLocked()
	}
	sess.mu.Unlock()
	for _, cancel := range staleCancels {
		if cancel != nil {
			go cancel()
		}
	}
	return nil
}

func (sess *session) ensurePageContext(parent context.Context, id target.ID) (context.Context, error) {
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return nil, browserError(ErrSessionNotFound, "browser session is closed", "session", nil, nil)
	}
	if _, ok := sess.pages[id]; !ok {
		sess.mu.Unlock()
		return nil, browserError(ErrPageNotFound, "browser page was not found", "page", &ErrorDetails{PageID: string(id)}, nil)
	}
	if existing := sess.pageContexts[id]; existing != nil && existing.ctx.Err() == nil {
		ctx := existing.ctx
		sess.mu.Unlock()
		return ctx, nil
	}
	sess.mu.Unlock()

	pageCtx, pageCancel := chromedp.NewContext(sess.browserCtx, chromedp.WithTargetID(id))
	attachDone := make(chan error, 1)
	go func() { attachDone <- chromedp.Run(pageCtx) }()
	select {
	case err := <-attachDone:
		if err != nil {
			pageCancel()
			return nil, err
		}
	case <-parent.Done():
		pageCancel()
		return nil, parent.Err()
	}

	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		pageCancel()
		return nil, browserError(ErrSessionNotFound, "browser session is closed", "session", nil, nil)
	}
	if _, ok := sess.pages[id]; !ok {
		sess.mu.Unlock()
		pageCancel()
		return nil, browserError(ErrPageNotFound, "browser page was closed while attaching", "page", &ErrorDetails{PageID: string(id)}, nil)
	}
	sess.pageContexts[id] = &pageContext{ctx: pageCtx, cancel: pageCancel}
	sess.mu.Unlock()
	return pageCtx, nil
}

func (sess *session) selectPage(requested string) (target.ID, error) {
	if err := sess.refreshPages(); err != nil {
		return "", browserError(ErrCDPFailed, "refresh browser pages", "cdp", nil, err)
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	id := target.ID(strings.TrimSpace(requested))
	if id == "" {
		id = sess.activePage
	}
	if _, ok := sess.pages[id]; !ok {
		available := make([]string, 0, len(sess.pages))
		for pageID := range sess.pages {
			available = append(available, string(pageID))
		}
		sort.Strings(available)
		return "", browserError(ErrPageNotFound, "browser page was not found", "page", &ErrorDetails{PageID: requested, AvailablePageIDs: available}, nil)
	}
	return id, nil
}

func (sess *session) mostRecentPageLocked() target.ID {
	var selected target.ID
	var order uint64
	for id, page := range sess.pages {
		if selected == "" || page.Order > order {
			selected = id
			order = page.Order
		}
	}
	return selected
}

func (sess *session) pageSummaries(documentPage target.ID, documentURL, documentTitle string) []PageSummary {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	pages := make([]*pageState, 0, len(sess.pages))
	for _, page := range sess.pages {
		pages = append(pages, page)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Order < pages[j].Order })
	result := make([]PageSummary, 0, len(pages))
	for _, page := range pages {
		url, title := page.URL, page.Title
		if page.ID == documentPage {
			url, title = documentURL, documentTitle
		}
		result = append(result, PageSummary{PageID: string(page.ID), URL: url, Title: title, Active: page.ID == sess.activePage})
	}
	return result
}

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
