import fs from 'node:fs/promises';
import path from 'node:path';
import crypto from 'node:crypto';

const payload = JSON.parse(process.env.BROWSER_RUNNER_PAYLOAD || '{}');
const operation = payload.operation;
const args = payload.args || {};
const artifactDir = payload.artifact_dir || process.env.BROWSER_ARTIFACT_DIR || '.';
const stateFile = path.join(artifactDir, 'browser-state.json');
const serverUrl = (process.env.AGENTDOCK_SERVER_URL || '').replace(/\/+$/, '');

// session_start 只写入状态，不应该因为本机尚未安装 Playwright 依赖而失败。
// 只有真正打开页面时才延迟加载浏览器驱动。
async function loadChromium() {
  const playwright = await import('playwright-core');
  return playwright.chromium;
}

function channelForSession(session) {
  if (session.channel) return session.channel;
  if (session.browser === 'edge' || session.browser === 'msedge') return 'msedge';
  if (session.browser === 'chrome') return 'chrome';
  return undefined;
}

async function readState() {
  try {
    return JSON.parse(await fs.readFile(stateFile, 'utf8'));
  } catch {
    return { sessions: {} };
  }
}

async function writeState(state) {
  await fs.mkdir(artifactDir, { recursive: true });
  await fs.writeFile(stateFile, JSON.stringify(state, null, 2));
}

function newSessionId() {
  return `browser-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

function safeProfileId(value) {
  const raw = String(value || '').trim();
  if (!raw) return '';
  return raw.replace(/[^a-zA-Z0-9_.-]/g, '-').slice(0, 80);
}

function normalizeLocalStorage(value) {
  if (!value) return [];
  if (Array.isArray(value)) {
    return value.map(item => ({
      origin: String(item?.origin || ''),
      values: item?.values && typeof item.values === 'object' ? item.values : item
    })).filter(item => item.origin && item.values && typeof item.values === 'object');
  }
  if (typeof value === 'object') {
    return Object.entries(value).map(([origin, values]) => ({ origin, values })).filter(item => item.origin && item.values && typeof item.values === 'object');
  }
  return [];
}

async function applyLocalStorage(page, localStorageConfig) {
  const entries = normalizeLocalStorage(localStorageConfig);
  let applied = 0;
  for (const entry of entries) {
    if (!page.url().startsWith(entry.origin)) continue;
    const ok = await page.evaluate(values => {
      for (const [key, value] of Object.entries(values)) {
        window.localStorage.setItem(key, typeof value === 'string' ? value : JSON.stringify(value));
      }
      return true;
    }, entry.values).catch(() => false);
    if (ok) applied++;
  }
  return applied;
}

async function collectPageMetrics(page, screenshotPath, fullPage) {
  const stat = await fs.stat(screenshotPath).catch(() => null);
  const viewport = page.viewportSize();
  const pageSize = await page.evaluate(() => ({
    width: Math.max(document.documentElement?.scrollWidth || 0, document.body?.scrollWidth || 0, window.innerWidth || 0),
    height: Math.max(document.documentElement?.scrollHeight || 0, document.body?.scrollHeight || 0, window.innerHeight || 0),
    device_pixel_ratio: window.devicePixelRatio || 1
  })).catch(() => ({}));
  const focus = await page.evaluate(() => {
    const el = document.activeElement;
    if (!el || el === document.body) return null;
    return {
      tag: el.tagName?.toLowerCase() || '',
      id: el.id || '',
      class: typeof el.className === 'string' ? el.className.slice(0, 120) : '',
      name: el.getAttribute?.('name') || '',
      placeholder: el.getAttribute?.('placeholder') || '',
      text: (el.innerText || el.textContent || '').trim().slice(0, 120)
    };
  }).catch(() => null);
  return {
    viewport,
    page_size: pageSize,
    focused_element: focus,
    screenshot_size_bytes: stat?.size || 0,
    screenshot_width: fullPage ? pageSize.width : viewport?.width,
    screenshot_height: fullPage ? pageSize.height : viewport?.height
  };
}

async function collectInteractiveElements(page, limit = 40) {
  return await page.evaluate(max => {
    const selector = 'a,button,input,textarea,select,[role="button"],[role="link"],[tabindex]';
    return Array.from(document.querySelectorAll(selector)).filter(el => {
      const style = window.getComputedStyle(el);
      const rect = el.getBoundingClientRect();
      return style.visibility !== 'hidden' && style.display !== 'none' && rect.width > 0 && rect.height > 0;
    }).slice(0, max).map((el, index) => ({
      index,
      tag: el.tagName.toLowerCase(),
      type: el.getAttribute('type') || '',
      role: el.getAttribute('role') || '',
      id: el.id || '',
      name: el.getAttribute('name') || '',
      placeholder: el.getAttribute('placeholder') || '',
      text: (el.innerText || el.value || el.getAttribute('aria-label') || el.textContent || '').trim().slice(0, 120)
    }));
  }, limit).catch(() => []);
}

async function attachImageIfRequested(result, screenshotPath) {
  if (args.include_image !== true && args.include_image_base64 !== true) return result;
  const maxBytes = args.max_image_bytes || 750000;
  const data = await fs.readFile(screenshotPath);
  if (data.length > maxBytes) {
    result.image_attached = false;
    result.image_warnings = [`screenshot image exceeds max_image_bytes (${data.length} > ${maxBytes})`];
    return result;
  }
  result.image_attached = true;
  result.image_mime_type = 'image/png';
  result.image_size_bytes = data.length;
  result.image_base64 = data.toString('base64');
  return result;
}

async function saveStorageStateIfNeeded(context, session, sessionId) {
  if (args.save_storage_state !== true && session.save_storage_state !== true) return {};
  const storageDir = path.join(artifactDir, 'storage-states');
  await fs.mkdir(storageDir, { recursive: true });
  const storageFile = `${safeProfileId(sessionId) || 'session'}-${Date.now()}.json`;
  const storagePath = path.join(storageDir, storageFile);
  await context.storageState({ path: storagePath });
  return { storage_state_path: storagePath, storage_state_file: storageFile };
}

async function closeSessionState(state, sessionId) {
  const existed = Boolean(state.sessions[sessionId]);
  delete state.sessions[sessionId];
  await writeState(state);
  return existed;
}

async function cleanupStaleSessions(state) {
  const maxAgeMs = args.max_age_ms ?? 6 * 60 * 60 * 1000;
  const now = Date.now();
  const removed = [];
  for (const [id, session] of Object.entries(state.sessions || {})) {
    const stamp = Date.parse(session.updated_at || session.created_at || 0);
    if (!stamp || now - stamp > maxAgeMs) {
      removed.push(id);
      delete state.sessions[id];
    }
  }
  await writeState(state);
  return removed;
}

async function closeWithTimeout(target, label, ms = 4000) {
  if (!target || typeof target.close !== 'function') return;
  let timer;
  await Promise.race([
    target.close(),
    new Promise(resolve => {
      timer = setTimeout(resolve, ms);
    })
  ]).catch(() => {});
  if (timer) clearTimeout(timer);
}

async function launchPage(session) {
  const chromium = await loadChromium();
  let browser;
  let context;
  let page;

  // CDP 只连接已经由用户显式开启调试端口的浏览器；普通浏览器不能被直接接管。
  if (session.backend === 'cdp') {
    if (!session.cdp_url) throw new Error('cdp_url is required when backend=cdp');
    browser = await chromium.connectOverCDP(session.cdp_url);
    context = browser.contexts()[0] || await browser.newContext({
      viewport: session.viewport || { width: 1280, height: 800 }
    });
    page = context.pages()[0] || await context.newPage();
  } else {
    const launchOptions = { headless: session.headless !== false };
    const channel = channelForSession(session);
    if (channel) launchOptions.channel = channel;
    if (session.profile_id) {
      const profileDir = path.join(artifactDir, 'profiles', safeProfileId(session.profile_id));
      context = await chromium.launchPersistentContext(profileDir, {
        ...launchOptions,
        viewport: session.viewport || { width: 1280, height: 800 }
      });
      browser = context.browser();
      page = context.pages()[0] || await context.newPage();
    } else {
      browser = await chromium.launch(launchOptions);
      context = await browser.newContext({
        viewport: session.viewport || { width: 1280, height: 800 },
        storageState: session.storage_state || undefined
      });
      page = await context.newPage();
    }
  }

  const consoleErrors = [];
  const networkErrors = [];
  const pageErrors = [];
  page.on('console', msg => {
    if (msg.type() === 'error') consoleErrors.push(msg.text());
  });
  page.on('pageerror', err => pageErrors.push(err.message));
  page.on('requestfailed', req => networkErrors.push(`${req.method()} ${req.url()} ${req.failure()?.errorText || ''}`));
  if (Array.isArray(session.cookies) && session.cookies.length) {
    await context.addCookies(session.cookies);
  }
  if (session.url && page.url() !== session.url) await page.goto(session.url, { waitUntil: 'domcontentloaded' });
  const localStorageApplied = await applyLocalStorage(page, session.local_storage);
  if (localStorageApplied > 0 && session.reload_after_local_storage !== false) {
    await page.reload({ waitUntil: 'domcontentloaded' }).catch(() => {});
  }
  return { browser, context, page, consoleErrors, networkErrors, pageErrors };
}

async function snapshot(page, extra = {}) {
  const screenshotDir = path.join(artifactDir, 'screenshots');
  await fs.mkdir(screenshotDir, { recursive: true });
  const screenshotFile = `snapshot-${Date.now()}.png`;
  const screenshotPath = path.join(screenshotDir, screenshotFile);
  const fullPage = args.full_page === true;
  await page.screenshot({ path: screenshotPath, fullPage });
  const text = (await page.locator('body').innerText({ timeout: 3000 }).catch(() => '')).slice(0, args.max_text_chars || 12000);
  const screenshotArtifactId = `browser-screenshot-${crypto.createHash('sha256').update(screenshotPath).digest('hex').slice(0, 16)}`;
  const metrics = await collectPageMetrics(page, screenshotPath, fullPage);
  const result = {
    url: page.url(),
    title: await page.title(),
    text,
    screenshot_path: screenshotPath,
    screenshot_file: screenshotFile,
    screenshot_artifact_id: screenshotArtifactId,
    artifact: {
      kind: 'browser_screenshot',
      path: screenshotPath,
      file: screenshotFile,
      artifact_id: screenshotArtifactId
    },
    interactive_elements: await collectInteractiveElements(page, args.max_interactive_elements || 40),
    ...metrics,
    ...extra
  };
  if (serverUrl) {
    result.screenshot_url = `${serverUrl}/artifacts/browser/screenshots/${encodeURIComponent(screenshotFile)}`;
    result.artifact.url = result.screenshot_url;
  }
  if (args.include_screenshot_base64 === true) {
    result.screenshot_mime_type = 'image/png';
    result.screenshot_base64 = await fs.readFile(screenshotPath, 'base64');
  }
  return await attachImageIfRequested(result, screenshotPath);
}

async function runActions(page, actions = []) {
  for (const action of actions) {
    const type = action.type;
    if (type === 'goto') await page.goto(action.url, { waitUntil: action.wait_until || 'domcontentloaded' });
    else if (type === 'click') await page.click(action.selector);
    else if (type === 'fill') await page.fill(action.selector, action.value ?? '');
    else if (type === 'press') await page.press(action.selector || 'body', action.key);
    else if (type === 'wait') await page.waitForTimeout(action.ms || 1000);
    else if (type === 'wait_for_selector') await page.waitForSelector(action.selector, { timeout: action.timeout_ms || 10000 });
    else if (type === 'select') await page.selectOption(action.selector, action.value);
    else if (type === 'scroll') await page.mouse.wheel(action.delta_x || 0, action.delta_y || 800);
    else if (type === 'reload') await page.reload({ waitUntil: action.wait_until || 'domcontentloaded' });
    else if (type === 'back') await page.goBack({ waitUntil: action.wait_until || 'domcontentloaded' });
    else if (type === 'forward') await page.goForward({ waitUntil: action.wait_until || 'domcontentloaded' });
    else if (type === 'evaluate') {
      throw new Error('evaluate action is disabled for browser safety');
    }
    else throw new Error(`Unsupported browser action: ${type}`);
  }
}

async function main() {
  const state = await readState();
  if (operation === 'session_start') {
    const sessionId = args.session_id || newSessionId();
    state.sessions[sessionId] = {
      session_id: sessionId,
      backend: args.backend || 'playwright',
      cdp_url: args.cdp_url || undefined,
      browser: args.browser || 'chromium',
      channel: args.channel || undefined,
      headless: args.headless !== false,
      viewport: args.viewport || { width: 1280, height: 800 },
      url: args.url || 'about:blank',
      profile_id: args.profile_id || undefined,
      storage_state: args.storage_state || undefined,
      local_storage: args.local_storage || undefined,
      cookies: Array.isArray(args.cookies) ? args.cookies : undefined,
      reload_after_local_storage: args.reload_after_local_storage !== false,
      save_storage_state: args.save_storage_state === true,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString()
    };
    await writeState(state);
    console.log(JSON.stringify({ ok: true, session_id: sessionId, status: 'created' }));
    return;
  }

  if (operation === 'session_cleanup') {
    const removed = await cleanupStaleSessions(state);
    console.log(JSON.stringify({ ok: true, status: 'cleaned', removed_count: removed.length, removed_sessions: removed }));
    return;
  }

  const sessionId = args.session_id;
  if (!sessionId || !state.sessions[sessionId]) throw new Error('Unknown or missing browser session_id');
  const session = state.sessions[sessionId];
  const env = await launchPage(session);
  try {
    if (operation === 'action') {
      await runActions(env.page, args.actions || []);
      session.url = env.page.url();
      session.updated_at = new Date().toISOString();
      const storageResult = await saveStorageStateIfNeeded(env.context, session, sessionId);
      const closeAfter = args.close_after === true;
      if (closeAfter) await closeSessionState(state, sessionId);
      else await writeState(state);
      console.log(JSON.stringify({ ok: true, session_id: sessionId, closed: closeAfter, ...storageResult, ...(await snapshot(env.page, { console_errors: env.consoleErrors, network_errors: env.networkErrors, page_errors: env.pageErrors })) }));
    } else if (operation === 'snapshot') {
      const storageResult = await saveStorageStateIfNeeded(env.context, session, sessionId);
      const closeAfter = args.close_after === true;
      if (closeAfter) await closeSessionState(state, sessionId);
      console.log(JSON.stringify({ ok: true, session_id: sessionId, closed: closeAfter, ...storageResult, ...(await snapshot(env.page, { console_errors: env.consoleErrors, network_errors: env.networkErrors, page_errors: env.pageErrors })) }));
    } else if (operation === 'session_close') {
      const existed = await closeSessionState(state, sessionId);
      console.log(JSON.stringify({ ok: true, session_id: sessionId, status: existed ? 'closed' : 'not_found' }));
    } else {
      throw new Error(`Unknown operation: ${operation}`);
    }
  } finally {
    // 有些持久化 profile / 系统浏览器在关闭上下文时会卡住，导致上层工具超时后 signal killed。
    // 这里把关闭动作限制在几秒内；状态已经在业务分支里写完，不能让资源回收阻塞结果返回。
    if (env.context && typeof env.context.close === 'function') await closeWithTimeout(env.context, 'context');
    else if (env.browser && typeof env.browser.close === 'function') await closeWithTimeout(env.browser, 'browser');
    setTimeout(() => process.exit(0), 50).unref();
  }
}

main().catch(err => {
  console.log(JSON.stringify({ ok: false, error: err.message, stack: err.stack }));
  process.exit(1);
});
