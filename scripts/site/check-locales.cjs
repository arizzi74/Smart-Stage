#!/usr/bin/env node
// Build first: python3 scripts/build-site.py
// Run locally: node scripts/site/check-locales.cjs
// Verify a deployment: node scripts/site/check-locales.cjs --base-url https://arizzi74.github.io/Smart-Stage/
// HTTP requests are read-only and restricted to the selected site's origin/path.
// Browser storage is isolated in temporary contexts; no existing browser data is used.
'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const http = require('node:http');
const path = require('node:path');
const { chromium } = require('../browser/node_modules/playwright');

const root = path.resolve(__dirname, '../..');
const siteRoot = path.join(root, 'dist', 'site');
const reportPath = path.join(root, 'dist', 'site-checks.json');
const preferenceKey = 'smartstage.website.language';
const commands = {
  'mac-command': 'curl -fsSL https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.sh | sh',
  'windows-command': 'irm https://raw.githubusercontent.com/arizzi74/Smart-Stage/main/install.ps1 | iex',
  'gateway-command': 'curl -fsSL https://github.com/arizzi74/Smart-Stage/releases/latest/download/install-gateway.sh | sh'
};
const messages = {
  en: {
    headline: ['Your show.', 'Ready on cue.'],
    copied: 'Copied',
    success: 'Command copied. Paste it into the terminal indicated above.'
  },
  it: {
    headline: ['Il tuo show.', 'Basta un tocco.'],
    copied: 'Copiato',
    success: 'Comando copiato. Incollalo nel terminale indicato sopra.',
    manual: 'Seleziona e copia il comando',
    unavailable: 'Accesso agli appunti non disponibile. Seleziona il comando e copialo manualmente.'
  }
};
const mimeTypes = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'application/javascript; charset=utf-8',
  '.json': 'application/json',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.txt': 'text/plain; charset=utf-8',
  '.md': 'text/markdown; charset=utf-8',
  '.xml': 'application/xml'
};

function parseArguments() {
  const args = process.argv.slice(2);
  if (args.length === 0) return null;
  assert.equal(args.length, 2, 'Usage: check-locales.cjs [--base-url URL]');
  assert.equal(args[0], '--base-url', 'Only --base-url is supported.');
  const url = new URL(args[1]);
  assert(['http:', 'https:'].includes(url.protocol), 'Base URL must use HTTP or HTTPS.');
  assert(!url.username && !url.password && !url.search && !url.hash,
    'Base URL must not contain credentials, a query or a fragment.');
  if (!url.pathname.endsWith('/')) url.pathname += '/';
  return url;
}

async function startServer() {
  // Fail clearly if the generated site has not been built yet.
  for (const file of ['index.html', 'en/index.html', 'it/index.html', 'site.js']) {
    await fs.access(path.join(siteRoot, file));
  }
  const prefix = '/Smart-Stage/';
  const server = http.createServer(async (request, response) => {
    if (!['GET', 'HEAD'].includes(request.method)) {
      response.writeHead(405).end();
      return;
    }
    try {
      const requested = new URL(request.url, 'http://127.0.0.1');
      if (!requested.pathname.startsWith(prefix)) {
        response.writeHead(404).end();
        return;
      }
      let filename = path.resolve(siteRoot, decodeURIComponent(requested.pathname.slice(prefix.length)));
      if (filename !== siteRoot && !filename.startsWith(siteRoot + path.sep)) {
        response.writeHead(403).end();
        return;
      }
      if ((await fs.stat(filename)).isDirectory()) filename = path.join(filename, 'index.html');
      const contents = await fs.readFile(filename);
      response.writeHead(200, {
        'Content-Type': mimeTypes[path.extname(filename)] || 'application/octet-stream',
        'Content-Length': contents.length,
        'Cache-Control': 'no-store'
      });
      response.end(request.method === 'HEAD' ? undefined : contents);
    } catch {
      response.writeHead(404).end();
    }
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  return { server, base: new URL('http://127.0.0.1:' + server.address().port + prefix) };
}

async function main() {
  let base = parseArguments();
  let server;
  let browser;
  const report = { startedAt: new Date().toISOString(), checks: [] };
  try {
    if (!base) ({ server, base } = await startServer());
    report.baseURL = base.href;
    report.mode = server ? 'local-built-site' : 'public-site-read-only';
    browser = await chromium.launch({ headless: true });
    report.browserVersion = browser.version();

    async function withPage(options, callback) {
      const originStorage = options.stored === undefined ? [] : [{
        origin: base.origin,
        localStorage: [{ name: preferenceKey, value: options.stored }]
      }];
      const context = await browser.newContext({
        locale: options.locale || 'en-US',
        javaScriptEnabled: options.javaScriptEnabled !== false,
        storageState: { cookies: [], origins: originStorage },
        serviceWorkers: 'block'
      });
      const errors = [];
      try {
        // Even live checks cannot follow external downloads, analytics or form submissions.
        await context.route('**/*', async route => {
          const request = route.request();
          const url = new URL(request.url());
          if (url.origin !== base.origin || !url.pathname.startsWith(base.pathname) ||
              !['GET', 'HEAD'].includes(request.method())) {
            errors.push('Blocked unexpected request: ' + request.method() + ' ' + request.url());
            await route.abort();
          } else {
            await route.continue();
          }
        });
        context.on('response', response => {
          if (response.status() >= 400) {
            errors.push('HTTP ' + response.status() + ': ' + response.url());
          }
        });
        await context.addInitScript(settings => {
          if (settings.languages !== undefined) {
            Object.defineProperty(navigator, 'languages', {
              configurable: true, get: () => settings.languages
            });
          }
          if (settings.language !== undefined) {
            Object.defineProperty(navigator, 'language', {
              configurable: true, get: () => settings.language
            });
          }
          if (settings.storageThrows) {
            Object.defineProperty(window, 'localStorage', {
              configurable: true,
              get() { throw new DOMException('Storage disabled for this test', 'SecurityError'); }
            });
          }
          if (settings.clipboardFailure) {
            Object.defineProperty(navigator, 'clipboard', {
              configurable: true,
              value: {
                writeText() {
                  return Promise.reject(new DOMException('Clipboard denied for this test', 'NotAllowedError'));
                }
              }
            });
          }
        }, {
          languages: options.languages,
          language: options.language,
          storageThrows: options.storageThrows,
          clipboardFailure: options.clipboardFailure
        });
        if (options.realClipboard) {
          await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: base.origin });
        }
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(15000);
        page.on('pageerror', error => errors.push(error.message));
        await callback(page);
        assert.deepEqual(errors, [], 'Unexpected browser errors or requests');
      } finally {
        await context.close();
      }
    }

    async function check(name, callback) {
      const started = Date.now();
      try {
        await callback();
        report.checks.push({ name, status: 'passed', durationMs: Date.now() - started });
        console.log('PASS ' + name);
      } catch (error) {
        report.checks.push({
          name, status: 'failed', durationMs: Date.now() - started,
          error: error.stack || String(error)
        });
        console.error('FAIL ' + name + '\n' + (error.stack || error));
      }
    }

    async function waitForLocale(page, language, suffix = '') {
      const expected = new URL(language + '/' + suffix, base).href;
      await page.waitForURL(expected, { waitUntil: 'load' });
      assert.equal(page.url(), expected, 'Language navigation must preserve the intended URL.');
      assert.equal(await page.locator('html').getAttribute('lang'), language);
    }

    async function assertContent(page, language) {
      const headline = await page.locator('#hero-heading').innerText();
      for (const phrase of messages[language].headline) assert(headline.includes(phrase), headline);
      for (const [id, command] of Object.entries(commands)) {
        assert.equal((await page.locator('#' + id).textContent()).trim(), command, id);
      }
    }

    const routing = [
      { name: 'Italian Italy', locale: 'it-IT', expected: 'it' },
      { name: 'Italian Switzerland', locale: 'it-CH', expected: 'it' },
      { name: 'English Britain', locale: 'en-GB', expected: 'en' },
      { name: 'Unsupported French falls back to English', locale: 'fr-FR', expected: 'en' },
      { name: 'First supported language in ordered preferences', languages: ['de-DE', 'it-CH', 'en'], expected: 'it' },
      { name: 'English preferred before Italian', languages: ['en', 'it'], expected: 'en' },
      { name: 'Empty languages falls back to navigator.language', languages: [], language: 'it-CH', expected: 'it' },
      { name: 'Stored English overrides Italian browser', locale: 'it-IT', stored: 'en', expected: 'en' },
      { name: 'Stored Italian overrides English browser', locale: 'en-US', stored: 'it', expected: 'it' },
      { name: 'Invalid stored preference is ignored', locale: 'it-IT', stored: 'invalid', expected: 'it' }
    ];
    for (const options of routing) {
      await check('Root: ' + options.name, () => withPage(options, async page => {
        const suffix = '?from=locale-check#install';
        await page.goto(new URL(suffix, base).href);
        await waitForLocale(page, options.expected, suffix);
        await assertContent(page, options.expected);
      }));
    }

    await check('Storage denied: root fallback and manual language switch remain usable', () =>
      withPage({ locale: 'it-IT', storageThrows: true }, async page => {
        const suffix = '?from=storage-denied#install';
        await page.goto(new URL(suffix, base).href);
        await waitForLocale(page, 'it', suffix);
        await page.locator('[data-language="en"]').click();
        await waitForLocale(page, 'en', suffix);
      }));

    for (const language of ['en', 'it']) {
      const opposite = language === 'en' ? 'it' : 'en';
      await check('Explicit /' + language + '/ ignores opposite browser and saved preference', () =>
        withPage({ locale: opposite + '-' + (opposite === 'it' ? 'IT' : 'GB'), stored: opposite }, async page => {
          const suffix = '?from=explicit#install';
          await page.goto(new URL(language + '/' + suffix, base).href);
          await waitForLocale(page, language, suffix);
          await assertContent(page, language);
        }));
    }

    await check('Click and keyboard language changes preserve URL and persist across root visits', () =>
      withPage({ locale: 'it-IT', stored: 'en' }, async page => {
        const suffix = '?source=manual-switch&value=one%20two#install';
        await page.goto(new URL('en/' + suffix, base).href);
        await page.locator('[data-language="it"]').click();
        await waitForLocale(page, 'it', suffix);
        assert.equal(await page.evaluate(key => localStorage.getItem(key), preferenceKey), 'it');
        await page.goto(new URL(suffix, base).href);
        await waitForLocale(page, 'it', suffix);
        const english = page.locator('[data-language="en"]');
        await english.focus();
        await page.keyboard.press('Enter');
        await waitForLocale(page, 'en', suffix);
        assert.equal(await page.evaluate(key => localStorage.getItem(key), preferenceKey), 'en');
        await page.goto(new URL(suffix, base).href);
        await waitForLocale(page, 'en', suffix);
      }));

    await check('JavaScript disabled: root language links and translated commands work', () =>
      withPage({ locale: 'it-IT', javaScriptEnabled: false }, async page => {
        for (const language of ['it', 'en']) {
          await page.goto(base.href);
          assert.equal(page.url(), base.href, 'Neutral page must remain usable without JavaScript.');
          for (const choice of ['en', 'it']) assert(await page.locator('[data-language="' + choice + '"]').isVisible());
          await page.locator('[data-language="' + language + '"]').click();
          await waitForLocale(page, language);
          await assertContent(page, language);
          for (const id of Object.keys(commands)) {
            assert(await page.locator('[data-copy="' + id + '"]').isHidden(), 'No nonfunctional copy controls without JavaScript.');
          }
        }
      }));

    for (const language of ['en', 'it']) {
      await check('Clipboard /' + language + '/: all three commands copied exactly with localized feedback', () =>
        withPage({ realClipboard: true }, async page => {
          await page.goto(new URL(language + '/', base).href);
          await assertContent(page, language);
          for (const [id, command] of Object.entries(commands)) {
            const button = page.locator('[data-copy="' + id + '"]');
            await button.click();
            await page.waitForFunction(({ id, feedback, label }) =>
              document.getElementById('copy-status').textContent === feedback &&
              document.querySelector('[data-copy="' + id + '"]').textContent === label,
            { id, feedback: messages[language].success, label: messages[language].copied });
            assert.equal(await page.evaluate(() => navigator.clipboard.readText()), command, id);
            assert.equal(await button.textContent(), messages[language].copied);
            assert(await button.isEnabled());
          }
        }));
    }

    await check('Italian clipboard denial shows translated manual-copy feedback', () =>
      withPage({ clipboardFailure: true }, async page => {
        await page.goto(new URL('it/', base).href);
        const button = page.locator('[data-copy="mac-command"]');
        await button.click();
        await page.waitForFunction(expected =>
          document.getElementById('copy-status').textContent === expected,
        messages.it.unavailable);
        assert.equal(await button.textContent(), messages.it.manual);
        assert(await button.isEnabled());
        assert.equal((await page.locator('#mac-command').textContent()).trim(), commands['mac-command']);
      }));

    report.status = report.checks.every(item => item.status === 'passed') ? 'passed' : 'failed';
    if (report.status !== 'passed') process.exitCode = 1;
  } catch (error) {
    report.status = 'failed';
    report.setupError = error.stack || String(error);
    console.error(report.setupError);
    process.exitCode = 1;
  } finally {
    if (browser) await browser.close();
    if (server) {
      server.closeIdleConnections?.();
      await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
    }
    report.finishedAt = new Date().toISOString();
    await fs.mkdir(path.dirname(reportPath), { recursive: true });
    await fs.writeFile(reportPath, JSON.stringify(report, null, 2) + '\n');
    console.log(report.checks.filter(item => item.status === 'passed').length + '/' +
      report.checks.length + ' checks passed. Report: ' + reportPath);
  }
}

main().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
