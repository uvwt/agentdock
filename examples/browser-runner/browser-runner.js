import fs from 'node:fs/promises';
import fsSync from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { spawn, spawnSync } from 'node:child_process';
import net from 'node:net';
import http from 'node:http';

const payload = JSON.parse(process.env.BROWSER_RUNNER_PAYLOAD || '{}');
const operation = payload.operation;
const args = payload.args || {};
const artifactDir = payload.artifact_dir || process.env.BROWSER_ARTIFACT_DIR || '.';
const stateFile = path.join(artifactDir, 'browser-state.json');

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

function configuredBrowserExecutable(session) {
  const configured = String(process.env.AGENTDOCK_BROWSER_EXECUTABLE_PATH || '').trim();
  if (!configured || !fsSync.existsSync(configured)) return '';
  const browser = String(session.browser || '').toLowerCase();
  const channel = String(session.channel || '').toLowerCase();
  if (channel && channel !== 'chromium') return '';
  if (browser && browser !== 'chromium' && browser !== 'chrome-for-testing') return '';
  return configured;
}

function chromiumLaunchOptions(session, options = {}) {
  const launchOptions = { ...options };
  const channel = channelForSession(session);
  if (channel) launchOptions.channel = channel;
  const executablePath = configuredBrowserExecutable(session);
  if (executablePath) {
    launchOptions.executablePath = executablePath;
    delete launchOptions.channel;
  }
  return launchOptions;
}

function isPlaywrightBundledChromiumMissing(err) {
  const message = String(err?.message || err || '');
  return message.includes('Executable doesn')
    && /[\/]ms-playwright[\/](chromium|chromium_headless_shell)-/.test(message);
}

function compactPlaywrightMissingMessage(err) {
  const message = String(err?.message || err || '');
  const withoutBox = message
    .replace(/╔[\s\S]*?╝/g, '')
    .split('\n')
    .filter(line => !line.includes('Please run') && !(line.includes('npx playwright') && line.includes('install')) && !line.includes('Playwright Team'))
    .join('\n')
    .trim();
  return withoutBox || 'Playwright bundled Chromium executable is missing';
}

function playwrightBundledChromiumMissingPayload(err) {
  const detail = compactPlaywrightMissingMessage(err);
  const match = detail.match(/Executable doesn't exist at ([^\n]+)/);
  return {
    ok: false,
    code: 'PLAYWRIGHT_CHROMIUM_MISSING',
    error: 'Playwright bundled Chromium executable is missing.',
    detail,
    missing_executable: match ? match[1] : undefined,
    suggested_retry: { browser: 'chrome' },
    suppressed_install_hint: true
  };
}

function canFallbackToSystemChrome(session) {
  const browser = String(session.browser || '').toLowerCase();
  const channel = String(session.channel || '').toLowerCase();
  if (process.platform !== 'darwin' && process.platform !== 'win32') return false;
  if (channel) return false;
  // 只有默认 bundled Chromium 路径失败时才自动切系统 Chrome；用户显式指定浏览器时保留其选择。
  if (session.browser_defaulted === false) return false;
  return browser === '' || browser === 'chromium' || browser === 'chrome-for-testing';
}

async function systemChromeFallbackLaunch(session, launchOptions, cause) {
  if (!isPlaywrightBundledChromiumMissing(cause) || !canFallbackToSystemChrome(session)) return null;
  const executable = await platformBrowserPath({ browser: 'chrome' });
  if (!executable) return null;
  const options = { ...launchOptions, executablePath: executable };
  delete options.channel;
  return {
    options,
    info: {
      fallback: true,
      reason: 'PLAYWRIGHT_CHROMIUM_MISSING',
      from: 'playwright-chromium',
      to: 'system-chrome',
      executable
    }
  };
}

async function launchBrowserWithFallback(chromium, session, launchOptions) {
  try {
    return { browser: await chromium.launch(launchOptions), launchInfo: { fallback: false, browser: session.browser || 'chromium' } };
  } catch (err) {
    const fallback = await systemChromeFallbackLaunch(session, launchOptions, err);
    if (!fallback) throw err;
    return { browser: await chromium.launch(fallback.options), launchInfo: fallback.info };
  }
}

async function launchPersistentContextWithFallback(chromium, session, profileDir, launchOptions, contextOptions) {
  try {
    return { context: await chromium.launchPersistentContext(profileDir, { ...launchOptions, ...contextOptions }), launchInfo: { fallback: false, browser: session.browser || 'chromium' } };
  } catch (err) {
    const fallback = await systemChromeFallbackLaunch(session, launchOptions, err);
    if (!fallback) throw err;
    return { context: await chromium.launchPersistentContext(profileDir, { ...fallback.options, ...contextOptions }), launchInfo: fallback.info };
  }
}

function structuredErrorPayload(err) {
  if (isPlaywrightBundledChromiumMissing(err)) return playwrightBundledChromiumMissingPayload(err);
  return { ok: false, error: err.message, stack: err.stack };
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


async function platformBrowserPath(session) {
  const browser = String(session.browser || '').toLowerCase();
  const channel = String(session.channel || '').toLowerCase();
  const candidates = [];
  const configured = configuredBrowserExecutable(session);
  if (configured) candidates.push(configured);
  if (browser === 'chromium' || browser === 'chrome-for-testing' || channel === 'chromium') {
    try {
      const chromium = await loadChromium();
      candidates.push(chromium.executablePath());
    } catch {
      // Fall back to platform candidates below.
    }
  }
  if (process.platform === 'darwin') {
    if (browser === 'edge' || browser === 'msedge' || channel === 'msedge') {
      candidates.push('/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge');
    }
    if (browser === 'chrome' || channel === 'chrome') {
      candidates.push('/Applications/Google Chrome.app/Contents/MacOS/Google Chrome');
    }
    candidates.push('/Applications/Chromium.app/Contents/MacOS/Chromium');
    if (browser === 'system') {
      candidates.push('/Applications/Google Chrome.app/Contents/MacOS/Google Chrome');
      candidates.push('/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge');
    }
  } else if (process.platform === 'linux') {
    candidates.push('/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium', '/usr/bin/chromium-browser');
  } else if (process.platform === 'win32') {
    const programFiles = process.env.ProgramFiles || 'C:\Program Files';
    const programFilesX86 = process.env['ProgramFiles(x86)'] || 'C:\Program Files (x86)';
    const localAppData = process.env.LOCALAPPDATA || '';
    const chromePaths = [
      path.join(programFiles, 'Google', 'Chrome', 'Application', 'chrome.exe'),
      path.join(programFilesX86, 'Google', 'Chrome', 'Application', 'chrome.exe'),
      localAppData && path.join(localAppData, 'Google', 'Chrome', 'Application', 'chrome.exe')
    ];
    const edgePaths = [
      path.join(programFiles, 'Microsoft', 'Edge', 'Application', 'msedge.exe'),
      path.join(programFilesX86, 'Microsoft', 'Edge', 'Application', 'msedge.exe'),
      localAppData && path.join(localAppData, 'Microsoft', 'Edge', 'Application', 'msedge.exe')
    ];
    if (browser === 'edge' || browser === 'msedge' || channel === 'msedge') candidates.push(...edgePaths);
    if (browser === 'chrome' || channel === 'chrome') candidates.push(...chromePaths);
    if (browser === 'system' || browser === '') candidates.push(...chromePaths, ...edgePaths);
  }
  return candidates.find(candidate => {
    try { return candidate && fsSync.existsSync(candidate); } catch { return false; }
  }) || '';
}

function findFreePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      const port = address && typeof address === 'object' ? address.port : 0;
      server.close(() => resolve(port));
    });
    server.on('error', reject);
  });
}

async function waitForCDP(port, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  let lastError = '';
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`http://127.0.0.1:${port}/json/version`);
      if (res.ok) return await res.json();
      lastError = `HTTP ${res.status}`;
    } catch (err) {
      lastError = err.message;
    }
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error(`browser CDP endpoint did not become ready: ${lastError}`);
}

async function cdpJSON(cdpURL, pathPart) {
  const base = String(cdpURL || '').replace(/\/+$/, '');
  if (!base) throw new Error('cdp_url is required');
  const res = await fetch(`${base}${pathPart}`);
  if (!res.ok) throw new Error(`CDP HTTP ${res.status} for ${pathPart}`);
  return await res.json();
}

function cdpTimeout() {
  return Math.min(Number(args.timeout_ms || 10000), 8000);
}

function cdpCall(wsURL, method, params = {}, timeoutMs = 10000) {
  return new Promise((resolve, reject) => {
    const id = 1;
    const ws = new WebSocket(wsURL);
    const timer = setTimeout(() => {
      try { ws.close(); } catch {}
      reject(new Error(`CDP method timed out: ${method}`));
    }, timeoutMs);
    ws.onopen = () => ws.send(JSON.stringify({ id, method, params }));
    ws.onerror = () => {
      clearTimeout(timer);
      reject(new Error(`CDP websocket error: ${method}`));
    };
    ws.onmessage = event => {
      let msg;
      try { msg = JSON.parse(event.data); } catch { return; }
      if (msg.id !== id) return;
      clearTimeout(timer);
      try { ws.close(); } catch {}
      if (msg.error) reject(new Error(`${method}: ${msg.error.message || JSON.stringify(msg.error)}`));
      else resolve(msg.result || {});
    };
  });
}

async function cdpPageTarget(session) {
  const targets = await cdpJSON(session.cdp_url, '/json/list');
  const page = targets.find(item => item.type === 'page' && !String(item.url || '').startsWith('chrome://'))
    || targets.find(item => item.type === 'page');
  if (!page?.webSocketDebuggerUrl) throw new Error('No debuggable page target found');
  return page;
}

function storageStateDestinationFrom(requestArgs, session, sessionId) {
  const targetSkill = safeProfileId(requestArgs.state_target_skill || session.state_target_skill || '');
  let storageDir;
  let storageFile;
  if (targetSkill) {
    storageDir = path.join(path.dirname(artifactDir), 'skill-data', targetSkill);
    storageFile = 'storage_state.json';
  } else {
    storageDir = path.join(artifactDir, 'storage-states');
    storageFile = `${safeProfileId(sessionId) || 'session'}-${Date.now()}.json`;
  }
  return { targetSkill, storageDir, storageFile, storagePath: path.join(storageDir, storageFile) };
}

function storageStateDestination(session, sessionId) {
  return storageStateDestinationFrom(args, session, sessionId);
}

async function saveContextStorageState(context, requestArgs, session, sessionId) {
  const dest = storageStateDestinationFrom(requestArgs, session, sessionId);
  await fs.mkdir(dest.storageDir, { recursive: true, mode: 0o700 });
  await context.storageState({ path: dest.storagePath });
  await fs.chmod(dest.storagePath, 0o600).catch(() => {});
  return { storage_state_path: dest.storagePath, storage_state_file: dest.storageFile, state_target_skill: dest.targetSkill || undefined };
}

function normalizeCookieForStorageState(cookie) {
  const result = {
    name: String(cookie.name || ''),
    value: String(cookie.value || ''),
    domain: String(cookie.domain || ''),
    path: String(cookie.path || '/'),
    expires: typeof cookie.expires === 'number' ? cookie.expires : -1,
    httpOnly: Boolean(cookie.httpOnly),
    secure: Boolean(cookie.secure)
  };
  if (cookie.sameSite && ['Strict', 'Lax', 'None'].includes(cookie.sameSite)) result.sameSite = cookie.sameSite;
  return result;
}

async function saveCDPStorageStateIfNeeded(session, sessionId, target) {
  if (args.save_storage_state !== true && session.save_storage_state !== true) return {};
  await cdpCall(target.webSocketDebuggerUrl, 'Network.enable', {}, cdpTimeout()).catch(() => {});
  const cookiesResult = await cdpCall(target.webSocketDebuggerUrl, 'Network.getCookies', { urls: [target.url] }, cdpTimeout());
  const localStorageResult = await cdpCall(target.webSocketDebuggerUrl, 'Runtime.evaluate', {
    expression: `(() => { try { return Object.entries(localStorage).map(([name, value]) => ({ name, value: String(value) })); } catch { return []; } })()`,
    returnByValue: true
  }, cdpTimeout());
  let origin = '';
  try { origin = new URL(target.url).origin; } catch {}
  const localStorage = Array.isArray(localStorageResult?.result?.value) ? localStorageResult.result.value : [];
  const storageState = {
    cookies: Array.isArray(cookiesResult.cookies) ? cookiesResult.cookies.map(normalizeCookieForStorageState).filter(item => item.name && item.domain) : [],
    origins: origin ? [{ origin, localStorage }] : []
  };
  const dest = storageStateDestination(session, sessionId);
  await fs.mkdir(dest.storageDir, { recursive: true, mode: 0o700 });
  await fs.writeFile(dest.storagePath, JSON.stringify(storageState, null, 2));
  await fs.chmod(dest.storagePath, 0o600).catch(() => {});
  return { storage_state_path: dest.storagePath, storage_state_file: dest.storageFile, state_target_skill: dest.targetSkill || undefined };
}

async function runCDPActions(session, actions = []) {
  if (!Array.isArray(actions) || actions.length === 0) return;
  let target = await cdpPageTarget(session);
  for (const action of actions) {
    const type = action.action;
    if (type === 'wait') {
      await new Promise(resolve => setTimeout(resolve, action.value ?? 1000));
    } else if (type === 'goto') {
      await cdpCall(target.webSocketDebuggerUrl, 'Page.navigate', { url: action.url }, cdpTimeout());
      await new Promise(resolve => setTimeout(resolve, action.value ?? 1500));
      target = await cdpPageTarget(session);
    } else if (type === 'reload') {
      await cdpCall(target.webSocketDebuggerUrl, 'Page.reload', {}, cdpTimeout());
      await new Promise(resolve => setTimeout(resolve, action.wait_ms || 1000));
    } else {
      throw new Error(`Unsupported CDP browser action: ${type}`);
    }
  }
}

async function cdpSnapshot(session, sessionId) {
  const target = await cdpPageTarget(session);
  const timeout = cdpTimeout();
  const textResult = await cdpCall(target.webSocketDebuggerUrl, 'Runtime.evaluate', {
    expression: `(() => document.body ? document.body.innerText : '')()`,
    returnByValue: true
  }, timeout).catch(() => ({ result: { value: '' } }));
  const titleResult = await cdpCall(target.webSocketDebuggerUrl, 'Runtime.evaluate', {
    expression: `(() => document.title || '')()`,
    returnByValue: true
  }, timeout).catch(() => ({ result: { value: target.title || '' } }));
  const text = String(textResult?.result?.value || '');
  const maxText = Number(args.max_text_chars || 8000);
  const storageResult = await saveCDPStorageStateIfNeeded(session, sessionId, target);
  const result = {
    ok: true,
    session_id: sessionId,
    closed: false,
    url: target.url || '',
    title: String(titleResult?.result?.value || target.title || ''),
    text: text.slice(0, maxText),
    text_length: text.length,
    artifact: {},
    console_errors: [],
    network_errors: [],
    page_errors: [],
    viewport: session.viewport || null,
    interactive_elements: []
  };
  if (shouldCaptureScreenshot(args)) {
    const screenshot = await cdpCall(target.webSocketDebuggerUrl, 'Page.captureScreenshot', { format: 'png' }, timeout).catch(() => null);
    if (screenshot?.data) {
      const screenshotDir = path.join(artifactDir, 'screenshots');
      await fs.mkdir(screenshotDir, { recursive: true });
      const screenshotFile = `snapshot-${Date.now()}.png`;
      const screenshotPath = path.join(screenshotDir, screenshotFile);
      await fs.writeFile(screenshotPath, Buffer.from(screenshot.data, 'base64'));
      result.screenshot_path = screenshotPath;
      result.screenshot_file = screenshotFile;
      result.artifact = { path: screenshotPath, mime_type: 'image/png' };
    }
  }
  return { ...storageResult, ...result };
}


async function daemonRequest(controlURL, action, body = {}, timeoutMs = 15000) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const res = await fetch(`${String(controlURL || '').replace(/\/+$/, '')}/${action}`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
      signal: controller.signal
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || `daemon HTTP ${res.status}`);
    return data;
  } finally {
    clearTimeout(timer);
  }
}

async function waitForDaemon(controlURL, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  let lastError = '';
  while (Date.now() < deadline) {
    try {
      const res = await daemonRequest(controlURL, 'status', {}, 2000);
      if (res.ok) return res;
      lastError = res.error || 'not ready';
    } catch (err) {
      lastError = err.message;
    }
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error(`browser profile daemon did not become ready: ${lastError}`);
}

async function daemonPageState(page, captureArgs = {}, extra = {}) {
  return await capturePageState(page, { ...captureArgs, capture_screenshot: captureArgs.capture_screenshot === true }, {
    ok: true,
    console_errors: [],
    network_errors: [],
    page_errors: [],
    ...extra
  });
}

async function runProfileDaemon() {
  const session = args.session || args;
  const sessionId = session.session_id || args.session_id || 'profile-daemon';
  const chromium = await loadChromium();
  const profileId = safeProfileId(session.profile_id || sessionId) || sessionId;
  const profileDir = path.join(artifactDir, 'visible-profiles', profileId);
  await fs.mkdir(profileDir, { recursive: true, mode: 0o700 });
  const context = await chromium.launchPersistentContext(profileDir, chromiumLaunchOptions(session, {
    headless: false,
    viewport: session.viewport || { width: 1280, height: 800 },
    args: ['--no-first-run', '--no-default-browser-check', '--disable-extensions', '--disable-component-extensions-with-background-pages', '--disable-features=Translate']
  }));
  const page = context.pages()[0] || await context.newPage();
  if (session.url && page.url() !== session.url) {
    await page.goto(session.url, { waitUntil: 'domcontentloaded', timeout: Number(session.timeout_ms || 30000) }).catch(() => {});
  }
  const server = http.createServer(async (req, res) => {
    const send = (status, data) => {
      res.writeHead(status, { 'content-type': 'application/json' });
      res.end(JSON.stringify(data));
    };
    try {
      let body = {};
      if (req.method === 'POST') {
        const chunks = [];
        for await (const chunk of req) chunks.push(chunk);
        if (chunks.length) body = JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}');
      }
      const action = new URL(req.url, 'http://127.0.0.1').pathname.replace(/^\//, '') || 'status';
      if (action === 'status') {
        send(200, { ...(await daemonPageState(page, body)), session_id: sessionId });
      } else if (action === 'action') {
        await runActions(page, body.actions || []);
        send(200, { ...(await daemonPageState(page, body)), session_id: sessionId });
      } else if (action === 'save') {
        const storageResult = await saveContextStorageState(context, { ...body, save_storage_state: true }, session, sessionId);
        send(200, { ...(await daemonPageState(page, body, storageResult)), session_id: sessionId });
      } else if (action === 'close') {
        send(200, { ok: true, session_id: sessionId, status: 'closed' });
        setTimeout(async () => {
          await context.close().catch(() => {});
          server.close(() => process.exit(0));
        }, 50).unref();
      } else {
        send(404, { ok: false, error: `unknown daemon action: ${action}` });
      }
    } catch (err) {
      send(500, { ok: false, error: err.message, stack: err.stack });
    }
  });
  await new Promise(resolve => server.listen(Number(args.control_port), '127.0.0.1', resolve));
}

async function launchProfileDaemon(session, sessionId) {
  const port = Number(session.cdp_port || 0) || await findFreePort();
  const controlURL = `http://127.0.0.1:${port}`;
  const daemonPayload = {
    operation: 'profile_daemon',
    args: { session: { ...session, session_id: sessionId }, control_port: port },
    artifact_dir: artifactDir
  };
  const child = spawn(process.execPath, [process.argv[1]], {
    detached: true,
    stdio: 'ignore',
    env: { ...process.env, BROWSER_RUNNER_PAYLOAD: JSON.stringify(daemonPayload), BROWSER_ARTIFACT_DIR: artifactDir }
  });
  child.unref();
  await waitForDaemon(controlURL, Number(session.timeout_ms || 25000));
  const profileId = safeProfileId(session.profile_id || sessionId) || sessionId;
  return {
    backend: 'daemon',
    control_url: controlURL,
    visible_process: {
      pid: child.pid,
      port,
      executable: 'playwright-chromium-daemon',
      profile_dir: path.join(artifactDir, 'visible-profiles', profileId),
      browser: 'playwright-chromium'
    }
  };
}

async function launchKeepOpenBrowser(session, sessionId) {
  const browser = String(session.browser || '').toLowerCase();
  const channel = String(session.channel || '').toLowerCase();
  if (browser === 'chromium' || browser === 'chrome-for-testing' || channel === 'chromium') {
    return await launchProfileDaemon(session, sessionId);
  }
  const executable = await platformBrowserPath(session);
  if (!executable) throw new Error('No visible Chrome/Chromium browser executable found');
  const port = Number(session.cdp_port || 0) || await findFreePort();
  const profileId = safeProfileId(session.profile_id || sessionId) || sessionId;
  const profileDir = path.join(artifactDir, 'visible-profiles', profileId);
  await fs.mkdir(profileDir, { recursive: true });
  const launchArgs = [
    `--remote-debugging-port=${port}`,
    '--remote-debugging-address=127.0.0.1',
    `--user-data-dir=${profileDir}`,
    '--no-first-run',
    '--no-default-browser-check',
    '--disable-extensions',
    '--disable-component-extensions-with-background-pages',
    '--new-window',
    '--disable-features=Translate',
    session.url || 'about:blank'
  ];
  let child;
  if (process.platform === 'darwin') {
    const appName = executable.includes('Google Chrome.app') ? 'Google Chrome' : executable.includes('Microsoft Edge.app') ? 'Microsoft Edge' : executable;
    child = spawn('/usr/bin/open', ['-na', appName, '--args', ...launchArgs], { detached: true, stdio: 'ignore' });
  } else {
    child = spawn(executable, launchArgs, { detached: true, stdio: 'ignore' });
  }
  child.unref();
  const version = await waitForCDP(port, Number(session.timeout_ms || 25000));
  return {
    backend: 'cdp',
    cdp_url: `http://127.0.0.1:${port}`,
    visible_process: {
      pid: child.pid,
      port,
      executable,
      profile_dir: profileDir,
      browser: version.Browser || '',
      user_agent: version['User-Agent'] || ''
    }
  };
}

function stopVisibleProcess(session) {
  const pid = Number(session?.visible_process?.pid || 0);
  if (!pid) return false;
  try {
    if (process.platform === 'win32') {
      return spawnSync('taskkill.exe', ['/PID', String(pid), '/T', '/F'], {
        stdio: 'ignore',
        windowsHide: true
      }).status === 0;
    }
    process.kill(pid, 'SIGTERM');
    return true;
  } catch {
    return false;
  }
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
  const stat = screenshotPath ? await fs.stat(screenshotPath).catch(() => null) : null;
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
  const result = {
    viewport,
    page_size: pageSize,
    focused_element: focus
  };
  if (screenshotPath) {
    result.screenshot_size_bytes = stat?.size || 0;
    result.screenshot_width = fullPage ? pageSize.width : viewport?.width;
    result.screenshot_height = fullPage ? pageSize.height : viewport?.height;
  }
  return result;
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

function shouldCaptureScreenshot(captureArgs = args) {
  return captureArgs.capture_screenshot !== false;
}

async function saveStorageStateIfNeeded(context, session, sessionId) {
  if (args.save_storage_state !== true && session.save_storage_state !== true) return {};
  return await saveContextStorageState(context, args, session, sessionId);
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
  let browserLaunch;

  // CDP 只连接已经由用户显式开启调试端口的浏览器；普通浏览器不能被直接接管。
  if (session.backend === 'cdp') {
    if (!session.cdp_url) throw new Error('cdp_url is required when backend=cdp');
    browser = await chromium.connectOverCDP(session.cdp_url);
    context = browser.contexts()[0] || await browser.newContext({
      viewport: session.viewport || { width: 1280, height: 800 }
    });
    page = context.pages()[0] || await context.newPage();
  } else {
    const launchOptions = chromiumLaunchOptions(session, { headless: session.headless !== false });
    if (session.profile_id) {
      const profileDir = path.join(artifactDir, 'profiles', safeProfileId(session.profile_id));
      const launched = await launchPersistentContextWithFallback(chromium, session, profileDir, launchOptions, {
        viewport: session.viewport || { width: 1280, height: 800 }
      });
      context = launched.context;
      browserLaunch = launched.launchInfo;
      browser = context.browser();
      page = context.pages()[0] || await context.newPage();
    } else {
      const launched = await launchBrowserWithFallback(chromium, session, launchOptions);
      browser = launched.browser;
      browserLaunch = launched.launchInfo;
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
  return { browser, context, page, consoleErrors, networkErrors, pageErrors, browserLaunch };
}

async function capturePageState(page, captureArgs = args, extra = {}) {
  const maxText = captureArgs.max_text_chars || 12000;
  const fullPage = captureArgs.full_page === true;
  const shouldCapture = shouldCaptureScreenshot(captureArgs);
  let screenshotPath = '';
  let screenshotFile = '';
  let screenshotArtifactId = '';
  let artifact = {};
  if (shouldCapture) {
    const screenshotDir = path.join(artifactDir, 'screenshots');
    await fs.mkdir(screenshotDir, { recursive: true });
    screenshotFile = `snapshot-${Date.now()}.png`;
    screenshotPath = path.join(screenshotDir, screenshotFile);
    await page.screenshot({ path: screenshotPath, fullPage });
    screenshotArtifactId = `browser-screenshot-${crypto.createHash('sha256').update(screenshotPath).digest('hex').slice(0, 16)}`;
    artifact = {
      kind: 'browser_screenshot',
      path: screenshotPath,
      file: screenshotFile,
      artifact_id: screenshotArtifactId
    };
  }
  const rawText = await page.locator('body').innerText({ timeout: 3000 }).catch(async () => {
    return await page.evaluate(() => document.body ? document.body.innerText : '').catch(() => '');
  });
  const text = String(rawText || '');
  const metrics = await collectPageMetrics(page, screenshotPath, fullPage);
  const result = {
    url: page.url(),
    title: await page.title().catch(() => ''),
    text: text.slice(0, maxText),
    text_length: text.length,
    artifact,
    interactive_elements: await collectInteractiveElements(page, captureArgs.max_interactive_elements || 40),
    ...metrics,
    ...extra
  };
  if (shouldCapture) {
    result.screenshot_path = screenshotPath;
    result.screenshot_file = screenshotFile;
    result.screenshot_artifact_id = screenshotArtifactId;
  }
  return result;
}

async function snapshot(page, extra = {}) {
  return await capturePageState(page, args, extra);
}

async function runActions(page, actions = []) {
  for (const action of actions) {
    const type = action.action;
    if (type === 'goto') await page.goto(action.url, { waitUntil: action.wait_until || 'domcontentloaded' });
    else if (type === 'click') await page.click(action.selector);
    else if (type === 'fill') await page.fill(action.selector, action.value ?? '');
    else if (type === 'press') await page.press(action.selector || 'body', action.key);
    else if (type === 'wait') await page.waitForTimeout(action.value ?? 1000);
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
  if (operation === 'profile_daemon') {
    await runProfileDaemon();
    return;
  }
  const state = await readState();
  if (operation === 'session_start') {
    const sessionId = args.session_id || newSessionId();
    state.sessions[sessionId] = {
      session_id: sessionId,
      backend: args.backend || 'playwright',
      cdp_url: args.cdp_url || undefined,
      browser: args.browser || 'chromium',
      browser_defaulted: !args.browser,
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
      state_target_skill: args.state_target_skill || undefined,
      keep_open: args.keep_open === true,
      cdp_port: args.cdp_port || undefined,
      timeout_ms: args.timeout_ms || undefined,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString()
    };
    let visible = {};
    if (state.sessions[sessionId].keep_open === true && state.sessions[sessionId].headless === false) {
      visible = await launchKeepOpenBrowser(state.sessions[sessionId], sessionId);
      state.sessions[sessionId] = { ...state.sessions[sessionId], ...visible };
    }
    await writeState(state);
    console.log(JSON.stringify({ ok: true, session_id: sessionId, status: 'created', keep_open: state.sessions[sessionId].keep_open, visible_process: state.sessions[sessionId].visible_process, backend: state.sessions[sessionId].backend, cdp_url: state.sessions[sessionId].cdp_url }));
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
  if (operation === 'session_close') {
    let daemonClosed = false;
    if (session.backend === 'daemon' && session.control_url) {
      daemonClosed = await daemonRequest(session.control_url, 'close', {}, 3000).then(() => true).catch(() => false);
    }
    const stopped = daemonClosed ? true : stopVisibleProcess(session);
    const existed = await closeSessionState(state, sessionId);
    console.log(JSON.stringify({ ok: true, session_id: sessionId, status: existed ? 'closed' : 'not_found', visible_process_stopped: stopped }));
    return;
  }
  if (session.backend === 'daemon') {
    if (operation === 'action') {
      const result = await daemonRequest(session.control_url, 'action', { ...args, actions: args.actions || [], max_text_chars: args.max_text_chars || 8000, capture_screenshot: shouldCaptureScreenshot(args) }, cdpTimeout());
      const closeAfter = args.close_after === true;
      if (closeAfter) await closeSessionState(state, sessionId);
      else await writeState(state);
      console.log(JSON.stringify({ ...result, closed: closeAfter }));
      return;
    }
    if (operation === 'snapshot') {
      const daemonAction = args.save_storage_state === true || session.save_storage_state === true ? 'save' : 'status';
      const result = await daemonRequest(session.control_url, daemonAction, { ...args, max_text_chars: args.max_text_chars || 8000, capture_screenshot: shouldCaptureScreenshot(args) }, cdpTimeout());
      const closeAfter = args.close_after === true;
      if (closeAfter) await closeSessionState(state, sessionId);
      else await writeState(state);
      console.log(JSON.stringify({ ...result, closed: closeAfter }));
      return;
    }
    throw new Error(`Unknown operation: ${operation}`);
  }
  if (session.backend === 'cdp') {
    if (operation === 'action') await runCDPActions(session, args.actions || []);
    if (operation === 'action' || operation === 'snapshot') {
      const closeAfter = args.close_after === true;
      const result = await cdpSnapshot(session, sessionId);
      if (closeAfter) await closeSessionState(state, sessionId);
      else await writeState(state);
      console.log(JSON.stringify({ ...result, closed: closeAfter }));
      return;
    }
    throw new Error(`Unknown operation: ${operation}`);
  }
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
      console.log(JSON.stringify({ ok: true, session_id: sessionId, closed: closeAfter, ...storageResult, ...(await snapshot(env.page, { console_errors: env.consoleErrors, network_errors: env.networkErrors, page_errors: env.pageErrors, browser_launch: env.browserLaunch })) }));
    } else if (operation === 'snapshot') {
      const storageResult = await saveStorageStateIfNeeded(env.context, session, sessionId);
      const closeAfter = args.close_after === true;
      if (closeAfter) await closeSessionState(state, sessionId);
      console.log(JSON.stringify({ ok: true, session_id: sessionId, closed: closeAfter, ...storageResult, ...(await snapshot(env.page, { console_errors: env.consoleErrors, network_errors: env.networkErrors, page_errors: env.pageErrors, browser_launch: env.browserLaunch })) }));
    } else {
      throw new Error(`Unknown operation: ${operation}`);
    }
  } finally {
    // 有些持久化 profile / 系统浏览器在关闭上下文时会卡住，导致上层工具超时后 signal killed。
    // 这里把关闭动作限制在几秒内；状态已经在业务分支里写完，不能让资源回收阻塞结果返回。
    if (session.keep_open === true && session.backend === 'cdp') {
      // Keep user-visible browser alive for interactive login; session_close owns shutdown.
    } else if (env.context && typeof env.context.close === 'function') await closeWithTimeout(env.context, 'context');
    else if (env.browser && typeof env.browser.close === 'function') await closeWithTimeout(env.browser, 'browser');
    setTimeout(() => process.exit(0), 50).unref();
  }
}

main().catch(err => {
  console.log(JSON.stringify(structuredErrorPayload(err)));
  process.exit(1);
});
