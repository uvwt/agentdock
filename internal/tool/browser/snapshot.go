package browser

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func (s *Service) snapshot(ctx context.Context, req SnapshotRequest) (Snapshot, error) {
	sess, err := s.getSession(req.SessionID)
	if err != nil {
		return Snapshot{}, err
	}
	sess.opMu.Lock()
	defer sess.opMu.Unlock()
	sess.touch(s.now())

	operationCtx, cancel := withTimeout(ctx, req.Timeout, 30*time.Second)
	defer cancel()
	pageID, err := sess.selectPage(req.PageID)
	if err != nil {
		return Snapshot{}, err
	}
	pageCtx, err := sess.ensurePageContext(operationCtx, pageID)
	if err != nil {
		return Snapshot{}, classifyOperationError(err, "page_attach")
	}
	listenCtx, listenCancel := context.WithCancel(pageCtx)
	defer listenCancel()
	diag := newDiagnostics()
	diag.attach(listenCtx)
	if err := diag.enable(operationCtx, pageCtx); err != nil {
		return Snapshot{}, classifyOperationError(err, "snapshot")
	}
	consoleErrors, networkErrors, pageErrors := diag.snapshot()
	snapshot, err := s.snapshotLocked(operationCtx, sess, req, consoleErrors, networkErrors, pageErrors)
	if err != nil {
		return Snapshot{}, err
	}
	if req.CloseAfter {
		if _, err := s.removeSession(req.SessionID); err != nil {
			return Snapshot{}, err
		}
		sess.stop()
		s.releaseSessionProfile(sess)
	}
	return snapshot, nil
}

func (s *Service) snapshotLocked(ctx context.Context, sess *session, req SnapshotRequest, consoleErrors []ConsoleError, networkErrors []NetworkError, pageErrors []PageError) (Snapshot, error) {
	pageID, err := sess.selectPage(req.PageID)
	if err != nil {
		return Snapshot{}, err
	}
	pageCtx, err := sess.ensurePageContext(ctx, pageID)
	if err != nil {
		return Snapshot{}, classifyOperationError(err, "page_attach")
	}

	maxText := req.MaxTextChars
	if maxText <= 0 {
		maxText = 8000
	}
	if maxText > 50000 {
		maxText = 50000
	}
	maxInteractive := req.MaxInteractiveElements
	if maxInteractive <= 0 {
		maxInteractive = 40
	}
	if maxInteractive > 200 {
		maxInteractive = 200
	}

	var state domSnapshot
	stateExpr := domSnapshotExpression(maxInteractive)
	if err := runWithContext(ctx, pageCtx, chromedp.Evaluate(stateExpr, &state)); err != nil {
		return Snapshot{}, classifyOperationError(err, "snapshot")
	}
	state.Text = truncateRunes(state.Text, maxText)

	var viewportWidth, viewportHeight int
	var pageWidth, pageHeight int
	var png []byte
	if err := runWithContext(ctx, pageCtx, chromedp.ActionFunc(func(execCtx context.Context) error {
		_, _, _, cssLayout, _, cssContent, err := page.GetLayoutMetrics().Do(execCtx)
		if err != nil {
			return err
		}
		if cssLayout != nil {
			viewportWidth = int(cssLayout.ClientWidth)
			viewportHeight = int(cssLayout.ClientHeight)
		}
		if cssContent != nil {
			pageWidth = int(cssContent.Width)
			pageHeight = int(cssContent.Height)
		}
		capture := page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatPng).WithFromSurface(true)
		if req.FullPage && cssContent != nil {
			capture = capture.WithCaptureBeyondViewport(true).WithClip(&page.Viewport{
				X: cssContent.X, Y: cssContent.Y, Width: cssContent.Width, Height: cssContent.Height, Scale: 1,
			})
		}
		png, err = capture.Do(execCtx)
		return err
	})); err != nil {
		return Snapshot{}, classifyOperationError(err, "screenshot")
	}
	if err := sess.refreshPages(); err != nil {
		return Snapshot{}, browserError(ErrCDPFailed, "refresh browser pages for snapshot", "cdp", nil, err)
	}
	// Snapshot 已读取到真实 document 状态，当前页输出以它为准，避免 TargetInfo 元数据滞后造成 pages 与主字段不一致。
	pages := sess.pageSummaries(pageID, state.URL, state.Title)
	if viewportWidth <= 0 {
		viewportWidth = state.Viewport.Width
	}
	if viewportHeight <= 0 {
		viewportHeight = state.Viewport.Height
	}
	if pageWidth <= 0 {
		pageWidth = state.PageSize.Width
	}
	if pageHeight <= 0 {
		pageHeight = state.PageSize.Height
	}

	return Snapshot{
		SessionID:           sess.id,
		PageID:              string(pageID),
		Pages:               pages,
		URL:                 state.URL,
		Title:               state.Title,
		Text:                state.Text,
		Viewport:            Viewport{Width: viewportWidth, Height: viewportHeight},
		PageSize:            Size{Width: pageWidth, Height: pageHeight},
		FocusedElement:      state.FocusedElement,
		InteractiveElements: state.InteractiveElements,
		ConsoleErrors:       nonNilConsole(consoleErrors),
		NetworkErrors:       nonNilNetwork(networkErrors),
		PageErrors:          nonNilPage(pageErrors),
		PNG:                 png,
	}, nil
}

type domSnapshot struct {
	URL                 string               `json:"url"`
	Title               string               `json:"title"`
	Text                string               `json:"text"`
	Viewport            Viewport             `json:"viewport"`
	PageSize            Size                 `json:"page_size"`
	FocusedElement      *FocusedElement      `json:"focused_element"`
	InteractiveElements []InteractiveElement `json:"interactive_elements"`
}

func domSnapshotExpression(maxInteractive int) string {
	return fmt.Sprintf(`(() => {
const norm = value => String(value || '').replace(/\s+/g, ' ').trim();
const visible = el => {
  const r=el.getBoundingClientRect(); if(r.width<=0 || r.height<=0) return false;
  for(let cur=el; cur; cur=cur.parentElement) { const s=getComputedStyle(cur); if(s.display==='none' || s.visibility==='hidden' || s.visibility==='collapse' || Number(s.opacity)===0) return false; }
  return true;
};
const selectorFor = el => {
  if (el.id) return '#' + CSS.escape(el.id);
  const parts=[]; let cur=el;
  while(cur && cur.nodeType===1 && parts.length<5) {
    let part=cur.tagName.toLowerCase();
    const parent=cur.parentElement;
    if(parent) { const siblings=Array.from(parent.children).filter(x=>x.tagName===cur.tagName); if(siblings.length>1) part += ':nth-of-type(' + (siblings.indexOf(cur)+1) + ')'; }
    parts.unshift(part); cur=parent;
  }
  return parts.join(' > ');
};
const describe = el => el ? {
  tag: el.tagName.toLowerCase(), id: el.id || '', name: el.getAttribute('name') || '', type: el.getAttribute('type') || '',
  text: norm(el.innerText || el.textContent).slice(0,120), value: 'value' in el ? String(el.value || '').slice(0,120) : '',
  aria_name: el.getAttribute('aria-label') || '', selector: selectorFor(el),
  is_editable: Boolean(el.isContentEditable || /^(input|textarea|select)$/i.test(el.tagName))
} : null;
const candidates=Array.from(document.querySelectorAll('a[href],button,input,textarea,select,[role="button"],[role="link"],[contenteditable="true"],[tabindex]'));
const interactive=candidates.filter(visible).slice(0,%d).map(el => ({
  tag:el.tagName.toLowerCase(), type:el.getAttribute('type')||'', text:norm(el.innerText || el.value || el.textContent).slice(0,120),
  aria_name:el.getAttribute('aria-label')||'', href:el.href||'', selector:selectorFor(el)
}));
const doc=document.documentElement; const body=document.body;
return {
  url:location.href, title:document.title, text:norm(body ? body.innerText : ''),
  viewport:{width:window.innerWidth,height:window.innerHeight},
  page_size:{width:Math.max(doc?.scrollWidth||0,body?.scrollWidth||0,window.innerWidth),height:Math.max(doc?.scrollHeight||0,body?.scrollHeight||0,window.innerHeight)},
  focused_element:describe(document.activeElement), interactive_elements:interactive
};
})()`, maxInteractive)
}

func truncateRunes(value string, max int) string {
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}

func nonNilConsole(values []ConsoleError) []ConsoleError {
	if values == nil {
		return []ConsoleError{}
	}
	return values
}

func nonNilNetwork(values []NetworkError) []NetworkError {
	if values == nil {
		return []NetworkError{}
	}
	return values
}

func nonNilPage(values []PageError) []PageError {
	if values == nil {
		return []PageError{}
	}
	return values
}
