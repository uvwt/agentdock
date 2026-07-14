import fs from 'node:fs/promises';
import fsSync from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { spawn, spawnSync } from 'node:child_process';
import net from 'node:net';
import http from 'node:http';

let payload = {};
try {
  payload = JSON.parse(process.env.BROWSER_RUNNER_PAYLOAD || '{}');
} catch (err) {
  process.stdout.write(JSON.stringify({
    ok: false,
    code: 'BROWSER_PAYLOAD_INVALID',
    error: {
      code: 'BROWSER_PAYLOAD_INVALID',
      message: 'browser runner payload is not valid JSON',
      phase: 'protocol',
      details: { reason: String(err?.message || err || '') }
    }
  }));
  process.exit(1);
}
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
    error: {
      code: 'PLAYWRIGHT_CHROMIUM_MISSING',
      message: 'Playwright bundled Chromium executable is missing.',
      phase: 'browser_launch',
      details: { detail, missing_executable: match ? match[1] : undefined }
    },
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
  const code = String(err?.code || 'BROWSER_OPERATION_FAILED');
  const message = String(err?.message || err || 'browser operation failed');
  return {
    ok: false,
    code,
    error: {
      code,
      message,
      phase: String(err?.phase || 'browser'),
      details: err?.details && typeof err.details === 'object' ? err.details : undefined
    },
    stack: err?.stack
  };
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

const playwrightPageIds = new WeakMap();
let playwrightPageSequence = 0;

function playwrightPageId(page) {
  if (!playwrightPageIds.has(page)) {
    playwrightPageSequence += 1;
    playwrightPageIds.set(page, `page-${playwrightPageSequence}`);
  }
  return playwrightPageIds.get(page);
}

async function describePlaywrightPages(context, activePage) {
  const contextPages = context.pages();
  const pages = contextPages.map(page => ({
    page_id: playwrightPageId(page),
    url: page.url(),
    title: '',
    active: page === activePage
  }));
  await Promise.all(pages.map(async (item, index) => {
    item.title = await contextPages[index]?.title().catch(() => '') || '';
  }));
  return pages;
}

function selectPlaywrightPage(context, requestedPageId, fallbackPage) {
  const requested = String(requestedPageId || '').trim();
  if (!requested) return fallbackPage || context.pages()[0];
  const page = context.pages().find(candidate => playwrightPageId(candidate) === requested);
  if (!page) {
    const err = new Error(`Unknown browser page_id: ${requested}`);
    err.code = 'BROWSER_PAGE_NOT_FOUND';
    err.details = { page_id: requested, available_page_ids: context.pages().map(playwrightPageId) };
    throw err;
  }
  return page;
}

function safeProfileId(value) {
  const raw = String(value || '').trim();
  if (!raw) return '';
  return raw.replace(/[^a-zA-Z0-9_.-]/g, '-').slice(0, 80);
}

function browserProfileLocation(session, sessionId) {
  const profileId = safeProfileId(session.profile_id || sessionId) || sessionId;
  const persistent = Boolean(session.profile_id);
  const directory = persistent
    ? (session.headless === false ? 'visible-profiles' : 'profiles')
    : 'session-profiles';
  return {
    profileDir: path.join(artifactDir, directory, profileId),
    persistent
  };
}

async function removeEphemeralProfile(session) {
  if (session.profile_id) return false;
  const profileDir = String(session?.visible_process?.profile_dir || '').trim();
  if (!profileDir) return false;
  await fs.rm(profileDir, { recursive: true, force: true }).catch(() => {});
  return !fsSync.existsSync(profileDir);
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

function daemonTimeout(requestArgs = args) {
  const operationTimeout = Number(requestArgs.timeout_ms || 30000);
  const actionTimeout = Array.isArray(requestArgs.actions)
    ? Math.max(0, ...requestArgs.actions.map(action => Number(action?.timeout_ms || 0)))
    : 0;
  return Math.min(Math.max(operationTimeout, actionTimeout + 2000), 300000);
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

async function createCDPResponseMonitor(target) {
  const ws = new WebSocket(target.webSocketDebuggerUrl);
  const requests = new Map();
  const responses = [];
  const waiters = new Set();
  let closed = false;

  const settleWaiter = (waiter, response) => {
    clearTimeout(waiter.timer);
    waiters.delete(waiter);
    waiter.resolve(response);
  };

  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      try { ws.close(); } catch {}
      const err = new Error('Timed out enabling CDP network monitoring');
      err.code = 'BROWSER_CDP_MONITOR_FAILED';
      reject(err);
    }, 10000);
    ws.onopen = () => ws.send(JSON.stringify({ id: 1, method: 'Network.enable', params: {} }));
    ws.onerror = () => {
      clearTimeout(timer);
      const err = new Error('CDP network monitor websocket error');
      err.code = 'BROWSER_CDP_MONITOR_FAILED';
      reject(err);
    };
    ws.onmessage = event => {
      let message;
      try { message = JSON.parse(event.data); } catch { return; }
      if (message.id === 1) {
        clearTimeout(timer);
        if (message.error) {
          const err = new Error(`Network.enable: ${message.error.message || JSON.stringify(message.error)}`);
          err.code = 'BROWSER_CDP_MONITOR_FAILED';
          reject(err);
        } else {
          resolve();
        }
        return;
      }
      if (message.method === 'Network.requestWillBeSent') {
        requests.set(message.params.requestId, String(message.params.request?.method || ''));
        return;
      }
      if (message.method !== 'Network.responseReceived') return;
      const response = {
        url: String(message.params.response?.url || ''),
        status: Number(message.params.response?.status || 0),
        method: requests.get(message.params.requestId) || ''
      };
      responses.push(response);
      if (responses.length > 200) responses.shift();
      for (const waiter of [...waiters]) {
        try {
          if (responseMatches(response, waiter.action)) settleWaiter(waiter, response);
        } catch (err) {
          clearTimeout(waiter.timer);
          waiters.delete(waiter);
          waiter.reject(err);
        }
      }
    };
  });

  return {
    wait(action) {
      const existing = responses.findLast(response => responseMatches(response, action));
      if (existing) return Promise.resolve(existing);
      return new Promise((resolve, reject) => {
        const waiter = { action, resolve, reject, timer: null };
        waiter.timer = setTimeout(() => {
          waiters.delete(waiter);
          const err = new Error('Timed out waiting for matching network response');
          err.code = 'BROWSER_WAIT_TIMEOUT';
          err.details = {
            url: action.url || undefined,
            url_pattern: action.url_pattern || undefined,
            method: action.method || undefined,
            status: action.status || undefined
          };
          reject(err);
        }, Number(action.timeout_ms || 10000));
        waiters.add(waiter);
      });
    },
    close() {
      if (closed) return;
      closed = true;
      for (const waiter of waiters) {
        clearTimeout(waiter.timer);
        waiter.reject(Object.assign(new Error('CDP response monitor closed'), { code: 'BROWSER_CDP_MONITOR_CLOSED' }));
      }
      waiters.clear();
      try { ws.close(); } catch {}
    }
  };
}

async function cdpPageTargets(session) {
  const targets = await cdpJSON(session.cdp_url, '/json/list');
  return targets.filter(item => item.type === 'page');
}

async function cdpPageTarget(session, requestedPageId = '') {
  const pages = await cdpPageTargets(session);
  const requested = String(requestedPageId || '').trim();
  const page = requested
    ? pages.find(item => item.id === requested)
    : pages.find(item => !String(item.url || '').startsWith('chrome://')) || pages[0];
  if (!page?.webSocketDebuggerUrl) {
    const err = new Error(requested ? `Unknown browser page_id: ${requested}` : 'No debuggable page target found');
    err.code = requested ? 'BROWSER_PAGE_NOT_FOUND' : 'BROWSER_PAGE_UNAVAILABLE';
    err.details = { page_id: requested || undefined, available_page_ids: pages.map(item => item.id) };
    throw err;
  }
  return page;
}

function describeCDPPages(pages, activePage) {
  return pages.map(page => ({
    page_id: page.id,
    url: page.url || '',
    title: page.title || '',
    active: page.id === activePage?.id
  }));
}

function storageStateDestinationFrom(requestArgs, session, sessionId) {
  const requestedPath = String(requestArgs.storage_state_path || session.storage_state_path || '').trim();
  if (requestedPath) {
    const storagePath = path.isAbsolute(requestedPath) ? path.normalize(requestedPath) : path.resolve(artifactDir, requestedPath);
    return {
      targetSkill: '',
      storageDir: path.dirname(storagePath),
      storageFile: path.basename(storagePath),
      storagePath
    };
  }

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

async function loadStorageState(session) {
  if (session.storage_state && typeof session.storage_state === 'object') return session.storage_state;
  const storagePath = String(session.storage_state_path || '').trim();
  if (!storagePath) return undefined;

  const content = await fs.readFile(storagePath, 'utf8');
  const state = JSON.parse(content);
  if (!state || typeof state !== 'object' || !Array.isArray(state.cookies) || !Array.isArray(state.origins)) {
    const err = new Error('storage_state_path does not contain a valid Playwright storage state');
    err.code = 'BROWSER_STORAGE_STATE_INVALID';
    err.details = { storage_state_path: storagePath };
    throw err;
  }
  return state;
}

async function applyStorageStateToPersistentContext(context, storageState) {
  if (!storageState) return;
  if (Array.isArray(storageState.cookies) && storageState.cookies.length) {
    await context.addCookies(storageState.cookies);
  }
  const origins = Array.isArray(storageState.origins) ? storageState.origins : [];
  if (origins.length) {
    const localStorageByOrigin = Object.fromEntries(origins.map(item => [item.origin, item.localStorage || []]));
    await context.addInitScript(valuesByOrigin => {
      const entries = valuesByOrigin[window.location.origin] || [];
      for (const item of entries) window.localStorage.setItem(item.name, item.value);
    }, localStorageByOrigin);
  }
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

async function applyCDPStorageState(session) {
  const storageState = await loadStorageState(session);
  if (!storageState) return;

  const target = await cdpPageTarget(session);
  const timeout = cdpTimeout();
  const cookies = storageState.cookies.map(normalizeCookieForStorageState).filter(item => item.name && item.domain);
  if (cookies.length) {
    await cdpCall(target.webSocketDebuggerUrl, 'Network.enable', {}, timeout).catch(() => {});
    await cdpCall(target.webSocketDebuggerUrl, 'Network.setCookies', { cookies }, timeout);
  }

  const localStorageByOrigin = Object.fromEntries(
    storageState.origins.map(item => [item.origin, item.localStorage || []]).filter(([origin]) => origin)
  );
  if (Object.keys(localStorageByOrigin).length) {
    const source = `(() => {
      const valuesByOrigin = ${JSON.stringify(localStorageByOrigin)};
      const entries = valuesByOrigin[window.location.origin] || [];
      for (const item of entries) window.localStorage.setItem(item.name, item.value);
    })()`;
    await cdpCall(target.webSocketDebuggerUrl, 'Page.addScriptToEvaluateOnNewDocument', { source }, timeout);
    await cdpCall(target.webSocketDebuggerUrl, 'Runtime.evaluate', { expression: source }, timeout).catch(() => {});
  }

  await cdpCall(target.webSocketDebuggerUrl, 'Page.reload', {}, timeout).catch(() => {});
  await new Promise(resolve => setTimeout(resolve, 500));
}

function textOrURLMatches(actual, expected) {
  const value = String(actual || '');
  const pattern = String(expected || '');
  if (!pattern) return false;
  if (!pattern.includes('*')) return value.includes(pattern);
  const escaped = pattern.replace(/[.+?^${}()|[\]\\]/g, '\\$&').replaceAll('*', '.*');
  return new RegExp(`^${escaped}$`).test(value);
}

async function pollUntil(check, timeoutMs, description) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await check()) return;
    await new Promise(resolve => setTimeout(resolve, 200));
  }
  const err = new Error(`Timed out waiting for ${description}`);
  err.code = 'BROWSER_WAIT_TIMEOUT';
  throw err;
}

async function runCDPActions(session, actions = [], requestedPageId = '') {
  if (!Array.isArray(actions) || actions.length === 0) return;
  let target = await cdpPageTarget(session, requestedPageId);
  const responseMonitor = actions.some(action => action.action === 'wait_for_response')
    ? await createCDPResponseMonitor(target)
    : null;
  try {
    for (const action of actions) {
      const type = action.action;
      if (type === 'wait') {
        await new Promise(resolve => setTimeout(resolve, action.value ?? 1000));
      } else if (type === 'goto') {
        await cdpCall(target.webSocketDebuggerUrl, 'Page.navigate', { url: action.url }, cdpTimeout());
        await new Promise(resolve => setTimeout(resolve, action.value ?? 1500));
        target = await cdpPageTarget(session, requestedPageId);
      } else if (type === 'reload') {
        await cdpCall(target.webSocketDebuggerUrl, 'Page.reload', {}, cdpTimeout());
        await new Promise(resolve => setTimeout(resolve, action.wait_ms || 1000));
      } else if (type === 'wait_for_url') {
        await pollUntil(async () => {
          target = await cdpPageTarget(session, requestedPageId);
          return textOrURLMatches(target.url, action.url);
        }, Number(action.timeout_ms || 10000), `URL ${action.url}`);
      } else if (type === 'wait_for_text') {
        const expected = String(action.text ?? action.value ?? '');
        await pollUntil(async () => {
          const result = await cdpCall(target.webSocketDebuggerUrl, 'Runtime.evaluate', {
            expression: `(() => document.body ? document.body.innerText : '')()`,
            returnByValue: true
          }, cdpTimeout()).catch(() => ({ result: { value: '' } }));
          return String(result?.result?.value || '').includes(expected);
        }, Number(action.timeout_ms || 10000), `text ${expected}`);
      } else if (type === 'wait_for_response') {
        await responseMonitor.wait(action);
      } else {
        throw new Error(`Unsupported CDP browser action: ${type}`);
      }
    }
  } finally {
    responseMonitor?.close();
  }
}

async function cdpSnapshot(session, sessionId, requestedPageId = '') {
  const pages = await cdpPageTargets(session);
  const target = await cdpPageTarget(session, requestedPageId);
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
    page_id: target.id,
    pages: describeCDPPages(pages, target),
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
    if (!res.ok) {
      const message = typeof data.error === 'string' ? data.error : data.error?.message || `daemon HTTP ${res.status}`;
      const err = new Error(message);
      err.code = data.code || data.error?.code || 'BROWSER_DAEMON_REQUEST_FAILED';
      err.details = data.error?.details;
      throw err;
    }
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
  const storageState = await loadStorageState(session);
  const { profileDir, persistent } = browserProfileLocation(session, sessionId);
  await fs.mkdir(profileDir, { recursive: true, mode: 0o700 });
  const launched = await launchPersistentContextWithFallback(chromium, session, profileDir, chromiumLaunchOptions(session), {
    headless: session.headless !== false,
    viewport: session.viewport || { width: 1280, height: 800 },
    args: ['--no-first-run', '--no-default-browser-check', '--disable-extensions', '--disable-component-extensions-with-background-pages', '--disable-features=Translate']
  });
  const context = launched.context;
  await applyStorageStateToPersistentContext(context, storageState);
  if (Array.isArray(session.cookies) && session.cookies.length) {
    await context.addCookies(session.cookies);
  }
  let page = context.pages()[0] || await context.newPage();
  const responseLog = createResponseLog(context);
  let actionRequestCount = 0;
  const diagnostics = createBrowserDiagnostics(context);
  const pageExtra = extra => ({ ...diagnostics.snapshot(), browser_launch: launched.launchInfo, ...extra });
  if (session.url && page.url() !== session.url) {
    // daemon 必须在父进程启动等待结束前进入可服务状态，初始页面不能独占整个操作超时。
    const startupBudget = Math.min(Number(session.timeout_ms || 30000), Number(session.startup_timeout_ms || 30000));
    const startupNavigationTimeout = Math.min(Math.max(startupBudget - 5000, 1000), 20000);
    await page.goto(session.url, { waitUntil: 'domcontentloaded', timeout: startupNavigationTimeout }).catch(() => {});
  }
  const localStorageApplied = await applyLocalStorage(page, session.local_storage);
  if (localStorageApplied > 0 && session.reload_after_local_storage !== false) {
    await page.reload({ waitUntil: 'domcontentloaded' }).catch(() => {});
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
        page = selectPlaywrightPage(context, body.page_id, page);
        send(200, { ...(await daemonPageState(page, body, pageExtra())), session_id: sessionId });
      } else if (action === 'action') {
        page = selectPlaywrightPage(context, body.page_id, page);
        if (actionRequestCount > 0) responseLog.length = 0;
        actionRequestCount++;
        page = await runActions(context, page, body.actions || [], responseLog);
        const storageResult = body.save_storage_state === true || session.save_storage_state === true
          ? await saveContextStorageState(context, body, session, sessionId)
          : {};
        send(200, { ...(await daemonPageState(page, body, pageExtra(storageResult))), session_id: sessionId });
      } else if (action === 'save') {
        page = selectPlaywrightPage(context, body.page_id, page);
        const storageResult = await saveContextStorageState(context, { ...body, save_storage_state: true }, session, sessionId);
        send(200, { ...(await daemonPageState(page, body, pageExtra(storageResult))), session_id: sessionId });
      } else if (action === 'close') {
        send(200, { ok: true, session_id: sessionId, status: 'closed' });
        setTimeout(async () => {
          await context.close().catch(() => {});
          if (!persistent) await fs.rm(profileDir, { recursive: true, force: true }).catch(() => {});
          server.close(() => process.exit(0));
        }, 50).unref();
      } else {
        send(404, { ok: false, error: `unknown daemon action: ${action}` });
      }
    } catch (err) {
      send(500, structuredErrorPayload(err));
    }
  });
  await new Promise(resolve => server.listen(Number(args.control_port), '127.0.0.1', resolve));
}

async function launchProfileDaemon(session, sessionId) {
  const port = Number(session.cdp_port || 0) || await findFreePort();
  const controlURL = `http://127.0.0.1:${port}`;
  const daemonPayload = {
    operation: 'profile_daemon',
    args: {
      session: { ...session, session_id: sessionId, startup_timeout_ms: Number(args.timeout_ms || 30000) },
      control_port: port
    },
    artifact_dir: artifactDir
  };
  const child = spawn(process.execPath, [process.argv[1]], {
    detached: true,
    stdio: 'ignore',
    env: { ...process.env, BROWSER_RUNNER_PAYLOAD: JSON.stringify(daemonPayload), BROWSER_ARTIFACT_DIR: artifactDir }
  });
  child.unref();
  try {
    const startupBudget = Math.min(Number(session.timeout_ms || 30000), Number(args.timeout_ms || 30000));
    await waitForDaemon(controlURL, Math.max(startupBudget - 1000, 1000));
  } catch (err) {
    await stopVisibleProcessCompletely({ visible_process: { pid: child.pid } });
    const failedProfile = browserProfileLocation(session, sessionId);
    if (!failedProfile.persistent) await fs.rm(failedProfile.profileDir, { recursive: true, force: true }).catch(() => {});
    err.code = err.code || 'BROWSER_DAEMON_START_FAILED';
    err.details = { ...(err.details || {}), control_url: controlURL };
    throw err;
  }
  const { profileDir } = browserProfileLocation(session, sessionId);
  return {
    backend: 'daemon',
    control_url: controlURL,
    visible_process: {
      pid: child.pid,
      port,
      executable: 'playwright-chromium-daemon',
      profile_dir: profileDir,
      browser: 'playwright-chromium'
    }
  };
}

async function ensurePlaywrightDaemon(state, sessionId, session) {
  if (session.backend !== 'playwright') return session;
  const daemon = await launchProfileDaemon(session, sessionId);
  const updated = { ...session, ...daemon, updated_at: new Date().toISOString() };
  state.sessions[sessionId] = updated;
  await writeState(state);
  return updated;
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
  const { profileDir } = browserProfileLocation({ ...session, headless: false }, sessionId);
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

async function stopVisibleProcessCompletely(session) {
  const pid = Number(session?.visible_process?.pid || 0);
  const stopped = stopVisibleProcess(session);
  if (!pid || process.platform === 'win32') return stopped;
  await new Promise(resolve => setTimeout(resolve, 200));
  try {
    process.kill(pid, 0);
    process.kill(pid, 'SIGKILL');
    return true;
  } catch {
    return stopped;
  }
}

async function closeCDPBrowser(session) {
  if (!session.cdp_url || !session.visible_process) return false;
  try {
    const version = await cdpJSON(session.cdp_url, '/json/version');
    const wsURL = version.webSocketDebuggerUrl;
    if (!wsURL) return false;
    return await new Promise(resolve => {
      const ws = new WebSocket(wsURL);
      let settled = false;
      let timer;
      const finish = value => {
        if (settled) return;
        settled = true;
        if (timer) clearTimeout(timer);
        try { ws.close(); } catch {}
        resolve(value);
      };
      timer = setTimeout(() => finish(false), 2000);
      ws.onopen = () => ws.send(JSON.stringify({ id: 1, method: 'Browser.close', params: {} }));
      ws.onmessage = event => {
        let message;
        try { message = JSON.parse(event.data); } catch { return; }
        if (message.id === 1) finish(!message.error);
      };
      ws.onclose = () => finish(true);
      ws.onerror = () => finish(false);
    });
  } catch {
    return false;
  }
}

async function closeOwnedBrowser(session) {
  let daemonClosed = false;
  let closed = false;
  if (session.backend === 'daemon' && session.control_url) {
    daemonClosed = await daemonRequest(session.control_url, 'close', {}, 3000).then(() => true).catch(() => false);
    closed = daemonClosed;
  } else if (session.backend === 'cdp') {
    closed = await closeCDPBrowser(session);
  }
  if (!closed) closed = await stopVisibleProcessCompletely(session);
  if (closed && !daemonClosed) {
    await new Promise(resolve => setTimeout(resolve, 250));
    await removeEphemeralProfile(session);
  }
  return closed;
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

function createResponseLog(context) {
  const responses = [];
  context.on('response', response => {
    responses.push({
      url: response.url(),
      status: response.status(),
      method: response.request().method(),
      timestamp: Date.now()
    });
    if (responses.length > 200) responses.shift();
  });
  return responses;
}

function createBrowserDiagnostics(context) {
  const consoleErrors = [];
  const networkErrors = [];
  const pageErrors = [];
  const pushBounded = (items, value) => {
    items.push(value);
    if (items.length > 100) items.shift();
  };
  const attach = page => {
    page.on('console', message => {
      if (message.type() === 'error') pushBounded(consoleErrors, message.text());
    });
    page.on('pageerror', error => pushBounded(pageErrors, error.message));
    page.on('requestfailed', request => {
      pushBounded(networkErrors, `${request.method()} ${request.url()} ${request.failure()?.errorText || ''}`);
    });
  };
  for (const page of context.pages()) attach(page);
  context.on('page', attach);
  return {
    snapshot: () => ({
      console_errors: [...consoleErrors],
      network_errors: [...networkErrors],
      page_errors: [...pageErrors]
    })
  };
}

function responseMatches(response, action) {
  const expectedURL = String(action.url || '').trim();
  const urlPattern = String(action.url_pattern || '').trim();
  const expectedMethod = String(action.method || '').trim().toUpperCase();
  const expectedStatus = Number(action.status || 0);
  if (expectedURL && !String(response.url || '').includes(expectedURL)) return false;
  if (urlPattern) {
    let regex;
    try {
      regex = new RegExp(urlPattern);
    } catch (err) {
      const invalid = new Error(`Invalid wait_for_response url_pattern: ${err.message}`);
      invalid.code = 'BROWSER_WAIT_PATTERN_INVALID';
      throw invalid;
    }
    if (!regex.test(String(response.url || ''))) return false;
  }
  if (expectedMethod && String(response.method || '').toUpperCase() !== expectedMethod) return false;
  if (expectedStatus && Number(response.status || 0) !== expectedStatus) return false;
  return true;
}

async function runBrowserWait(action, operation) {
  try {
    return await operation();
  } catch (err) {
    if (String(err?.name || '').includes('Timeout') || String(err?.message || '').includes('Timeout')) {
      err.code = 'BROWSER_WAIT_TIMEOUT';
      err.details = { action: action.action, timeout_ms: Number(action.timeout_ms || 10000) };
    }
    throw err;
  }
}

async function waitForResponse(context, responseLog, action) {
  const existing = responseLog.findLast(response => responseMatches(response, action));
  if (existing) return existing;

  return await runBrowserWait(action, async () => {
    const response = await context.waitForEvent('response', {
      timeout: Number(action.timeout_ms || 10000),
      predicate: candidate => responseMatches({
        url: candidate.url(),
        status: candidate.status(),
        method: candidate.request().method()
      }, action)
    });
    return { url: response.url(), status: response.status(), method: response.request().method() };
  });
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

async function closeSessionState(state, sessionId) {
  const existed = Boolean(state.sessions[sessionId]);
  delete state.sessions[sessionId];
  await writeState(state);
  return existed;
}

function touchBrowserSession(state, sessionId, session, result = {}) {
  const updated = {
    ...session,
    url: result.url || session.url,
    updated_at: new Date().toISOString()
  };
  state.sessions[sessionId] = updated;
  return updated;
}

async function cleanupStaleSessions(state) {
  const maxAgeMs = args.max_age_ms ?? 6 * 60 * 60 * 1000;
  const now = Date.now();
  const removed = [];
  for (const [id, session] of Object.entries(state.sessions || {})) {
    const stamp = Date.parse(session.updated_at || session.created_at || 0);
    if (!stamp || now - stamp > maxAgeMs) {
      await closeOwnedBrowser(session);
      removed.push(id);
      delete state.sessions[id];
    }
  }
  await writeState(state);
  return removed;
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
  const context = page.context();
  const result = {
    page_id: playwrightPageId(page),
    pages: await describePlaywrightPages(context, page),
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

async function runActions(context, page, actions = [], responseLog = []) {
  let activePage = page;
  for (const action of actions) {
    const type = action.action;
    if (activePage.isClosed()) activePage = context.pages().at(-1) || context.pages()[0];
    if (!activePage) throw new Error('No active browser page is available');

    if (type === 'goto') await activePage.goto(action.url, { waitUntil: action.wait_until || 'domcontentloaded' });
    else if (type === 'click') await activePage.click(action.selector);
    else if (type === 'fill') await activePage.fill(action.selector, action.value ?? '');
    else if (type === 'press') await activePage.press(action.selector || 'body', action.key);
    else if (type === 'wait') await activePage.waitForTimeout(action.value ?? 1000);
    else if (type === 'wait_for_selector') await runBrowserWait(action, () => activePage.waitForSelector(action.selector, { timeout: action.timeout_ms || 10000 }));
    else if (type === 'wait_for_url') {
      const expectedURL = String(action.url || '');
      const matcher = expectedURL.includes('*') ? expectedURL : url => url.href.includes(expectedURL);
      await runBrowserWait(action, () => activePage.waitForURL(matcher, { timeout: action.timeout_ms || 10000, waitUntil: action.wait_until || 'load' }));
    }
    else if (type === 'wait_for_text') {
      const text = String(action.text ?? action.value ?? '');
      if (!text) throw new Error('wait_for_text requires text or value');
      await runBrowserWait(action, () => activePage.getByText(text, { exact: action.exact === true }).first().waitFor({
        state: action.state || 'visible',
        timeout: action.timeout_ms || 10000
      }));
    }
    else if (type === 'wait_for_response') await waitForResponse(context, responseLog, action);
    else if (type === 'select') await activePage.selectOption(action.selector, action.value);
    else if (type === 'scroll') await activePage.mouse.wheel(action.delta_x || 0, action.delta_y || 800);
    else if (type === 'reload') await activePage.reload({ waitUntil: action.wait_until || 'domcontentloaded' });
    else if (type === 'back') await activePage.goBack({ waitUntil: action.wait_until || 'domcontentloaded' });
    else if (type === 'forward') await activePage.goForward({ waitUntil: action.wait_until || 'domcontentloaded' });
    else if (type === 'evaluate') {
      throw new Error('evaluate action is disabled for browser safety');
    }
    else throw new Error(`Unsupported browser action: ${type}`);
  }
  return activePage;
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
      storage_state_path: args.storage_state_path || undefined,
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
    if (state.sessions[sessionId].storage_state_path) await loadStorageState(state.sessions[sessionId]);
    let visible = {};
    if (state.sessions[sessionId].keep_open === true && state.sessions[sessionId].headless === false) {
      visible = await launchKeepOpenBrowser(state.sessions[sessionId], sessionId);
      state.sessions[sessionId] = { ...state.sessions[sessionId], ...visible };
    }
    if (state.sessions[sessionId].backend === 'cdp' && state.sessions[sessionId].storage_state_path) {
      await applyCDPStorageState(state.sessions[sessionId]);
    }
    await writeState(state);
    let pageInfo = { page_id: null, pages: [] };
    if (state.sessions[sessionId].backend === 'daemon' && state.sessions[sessionId].control_url) {
      const status = await daemonRequest(state.sessions[sessionId].control_url, 'status', {}, daemonTimeout());
      pageInfo = { page_id: status.page_id || null, pages: status.pages || [] };
    } else if (state.sessions[sessionId].backend === 'cdp' && state.sessions[sessionId].cdp_url) {
      const pages = await cdpPageTargets(state.sessions[sessionId]);
      const activePage = await cdpPageTarget(state.sessions[sessionId]);
      pageInfo = { page_id: activePage.id, pages: describeCDPPages(pages, activePage) };
    }
    console.log(JSON.stringify({ ok: true, session_id: sessionId, status: 'created', ...pageInfo, keep_open: state.sessions[sessionId].keep_open, visible_process: state.sessions[sessionId].visible_process, backend: state.sessions[sessionId].backend, cdp_url: state.sessions[sessionId].cdp_url }));
    return;
  }

  if (operation === 'session_cleanup') {
    const removed = await cleanupStaleSessions(state);
    console.log(JSON.stringify({ ok: true, status: 'cleaned', removed_count: removed.length, removed_sessions: removed }));
    return;
  }

  const sessionId = args.session_id;
  if (!sessionId || !state.sessions[sessionId]) throw new Error('Unknown or missing browser session_id');
  let session = state.sessions[sessionId];
  if (operation === 'session_close') {
    const stopped = await closeOwnedBrowser(session);
    const existed = await closeSessionState(state, sessionId);
    console.log(JSON.stringify({ ok: true, session_id: sessionId, status: existed ? 'closed' : 'not_found', visible_process_stopped: stopped }));
    return;
  }
  if (session.backend === 'playwright' && (operation === 'action' || operation === 'snapshot')) {
    session = await ensurePlaywrightDaemon(state, sessionId, session);
  }
  if (session.backend === 'daemon') {
    if (operation === 'action') {
      const result = await daemonRequest(session.control_url, 'action', { ...args, actions: args.actions || [], max_text_chars: args.max_text_chars || 8000, capture_screenshot: shouldCaptureScreenshot(args) }, daemonTimeout(args));
      const closeAfter = args.close_after === true;
      if (closeAfter) {
        await closeOwnedBrowser(session);
        await closeSessionState(state, sessionId);
      }
      else {
        session = touchBrowserSession(state, sessionId, session, result);
        await writeState(state);
      }
      console.log(JSON.stringify({ ...result, closed: closeAfter }));
      return;
    }
    if (operation === 'snapshot') {
      const daemonAction = args.save_storage_state === true || session.save_storage_state === true ? 'save' : 'status';
      const result = await daemonRequest(session.control_url, daemonAction, { ...args, max_text_chars: args.max_text_chars || 8000, capture_screenshot: shouldCaptureScreenshot(args) }, daemonTimeout(args));
      const closeAfter = args.close_after === true;
      if (closeAfter) {
        await closeOwnedBrowser(session);
        await closeSessionState(state, sessionId);
      }
      else {
        session = touchBrowserSession(state, sessionId, session, result);
        await writeState(state);
      }
      console.log(JSON.stringify({ ...result, closed: closeAfter }));
      return;
    }
    throw new Error(`Unknown operation: ${operation}`);
  }
  if (session.backend === 'cdp') {
    if (operation === 'action') await runCDPActions(session, args.actions || [], args.page_id || '');
    if (operation === 'action' || operation === 'snapshot') {
      const closeAfter = args.close_after === true;
      const result = await cdpSnapshot(session, sessionId, args.page_id || '');
      if (closeAfter) {
        await closeOwnedBrowser(session);
        await closeSessionState(state, sessionId);
      }
      else {
        session = touchBrowserSession(state, sessionId, session, result);
        await writeState(state);
      }
      console.log(JSON.stringify({ ...result, closed: closeAfter }));
      return;
    }
    throw new Error(`Unknown operation: ${operation}`);
  }
  throw new Error(`Unsupported browser backend: ${session.backend}`);
}

main().catch(err => {
  console.log(JSON.stringify(structuredErrorPayload(err)));
  process.exit(1);
});
