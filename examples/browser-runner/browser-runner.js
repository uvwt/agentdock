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
  const playwright = await import('playwright');
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
    browser = await chromium.launch(launchOptions);
    context = await browser.newContext({
      viewport: session.viewport || { width: 1280, height: 800 },
      storageState: session.storage_state || undefined
    });
    page = await context.newPage();
  }

  const consoleErrors = [];
  const networkErrors = [];
  const pageErrors = [];
  page.on('console', msg => {
    if (msg.type() === 'error') consoleErrors.push(msg.text());
  });
  page.on('pageerror', err => pageErrors.push(err.message));
  page.on('requestfailed', req => networkErrors.push(`${req.method()} ${req.url()} ${req.failure()?.errorText || ''}`));
  if (session.url && page.url() !== session.url) await page.goto(session.url, { waitUntil: 'domcontentloaded' });
  return { browser, context, page, consoleErrors, networkErrors, pageErrors };
}

async function snapshot(page, extra = {}) {
  const screenshotDir = path.join(artifactDir, 'screenshots');
  await fs.mkdir(screenshotDir, { recursive: true });
  const screenshotFile = `snapshot-${Date.now()}.png`;
  const screenshotPath = path.join(screenshotDir, screenshotFile);
  await page.screenshot({ path: screenshotPath, fullPage: args.full_page === true });
  const text = (await page.locator('body').innerText({ timeout: 3000 }).catch(() => '')).slice(0, args.max_text_chars || 12000);
  const screenshotArtifactId = `browser-screenshot-${crypto.createHash('sha256').update(screenshotPath).digest('hex').slice(0, 16)}`;
  const result = {
    url: page.url(),
    title: await page.title(),
    text,
    screenshot_path: screenshotPath,
    screenshot_artifact_id: screenshotArtifactId,
    ...extra
  };
  if (serverUrl) {
    result.screenshot_url = `${serverUrl}/artifacts/browser/screenshots/${encodeURIComponent(screenshotFile)}`;
  }
  if (args.include_screenshot_base64 === true) {
    result.screenshot_mime_type = 'image/png';
    result.screenshot_base64 = await fs.readFile(screenshotPath, 'base64');
  }
  return result;
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
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString()
    };
    await writeState(state);
    console.log(JSON.stringify({ ok: true, session_id: sessionId, status: 'created' }));
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
      await writeState(state);
      console.log(JSON.stringify({ ok: true, session_id: sessionId, ...(await snapshot(env.page, { console_errors: env.consoleErrors, network_errors: env.networkErrors, page_errors: env.pageErrors })) }));
    } else if (operation === 'snapshot') {
      console.log(JSON.stringify({ ok: true, session_id: sessionId, ...(await snapshot(env.page, { console_errors: env.consoleErrors, network_errors: env.networkErrors, page_errors: env.pageErrors })) }));
    } else if (operation === 'session_close') {
      delete state.sessions[sessionId];
      await writeState(state);
      console.log(JSON.stringify({ ok: true, session_id: sessionId, status: 'closed' }));
    } else {
      throw new Error(`Unknown operation: ${operation}`);
    }
  } finally {
    await env.browser.close();
  }
}

main().catch(err => {
  console.log(JSON.stringify({ ok: false, error: err.message, stack: err.stack }));
  process.exit(1);
});
