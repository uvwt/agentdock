package browser

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

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
