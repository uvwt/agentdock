import { chromium } from 'playwright';
import fs from 'node:fs/promises';
import path from 'node:path';

const payload = JSON.parse(process.env.BROWSER_RUNNER_PAYLOAD || '{}');
const operation = payload.operation;
const args = payload.args || {};
const artifactDir = payload.artifact_dir || process.env.BROWSER_ARTIFACT_DIR || '.';
const stateFile = path.join(artifactDir, 'browser-state.json');

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
  const browser = await chromium.launch({ headless: session.headless !== false });
  const context = await browser.newContext({
    viewport: session.viewport || { width: 1280, height: 800 },
    storageState: session.storage_state || undefined
  });
  const page = await context.newPage();
  const consoleErrors = [];
  const networkErrors = [];
  page.on('console', msg => {
    if (msg.type() === 'error') consoleErrors.push(msg.text());
  });
  page.on('requestfailed', req => networkErrors.push(`${req.method()} ${req.url()} ${req.failure()?.errorText || ''}`));
  if (session.url) await page.goto(session.url, { waitUntil: 'domcontentloaded' });
  return { browser, context, page, consoleErrors, networkErrors };
}

async function snapshot(page, extra = {}) {
  await fs.mkdir(path.join(artifactDir, 'screenshots'), { recursive: true });
  const screenshotPath = path.join(artifactDir, 'screenshots', `snapshot-${Date.now()}.png`);
  await page.screenshot({ path: screenshotPath, fullPage: args.full_page === true });
  const text = (await page.locator('body').innerText({ timeout: 3000 }).catch(() => '')).slice(0, args.max_text_chars || 12000);
  return {
    url: page.url(),
    title: await page.title(),
    text,
    screenshot_path: screenshotPath,
    ...extra
  };
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
    else if (type === 'evaluate') await page.evaluate(action.expression);
    else throw new Error(`Unsupported browser action: ${type}`);
  }
}

async function main() {
  const state = await readState();
  if (operation === 'session_start') {
    const sessionId = args.session_id || newSessionId();
    state.sessions[sessionId] = {
      session_id: sessionId,
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
      console.log(JSON.stringify({ ok: true, session_id: sessionId, ...(await snapshot(env.page, { console_errors: env.consoleErrors, network_errors: env.networkErrors })) }));
    } else if (operation === 'snapshot') {
      console.log(JSON.stringify({ ok: true, session_id: sessionId, ...(await snapshot(env.page, { console_errors: env.consoleErrors, network_errors: env.networkErrors })) }));
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
