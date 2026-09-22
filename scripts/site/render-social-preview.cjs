// From the repository root: node scripts/site/render-social-preview.cjs [en|it]
// Uses the existing development-only Playwright dependency in scripts/browser.
const fs = require('node:fs');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const { chromium } = require('../browser/node_modules/playwright');

(async () => {
  const root = path.resolve(__dirname, '../..');
  const language = process.argv[2] || 'en';
  if (!['en', 'it'].includes(language)) throw new Error('Choose en or it.');
  const suffix = language === 'it' ? '-it' : '';
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 640 }, deviceScaleFactor: 1 });
    await page.goto(pathToFileURL(path.join(__dirname, `social-preview${suffix}.html`)).href);
    await page.evaluate(() => Promise.all(Array.from(document.images, image => image.decode())));
    const output = path.join(root, `docs/site/assets/social-preview${suffix}.png`);
    fs.mkdirSync(path.dirname(output), { recursive: true });
    await page.screenshot({ path: output });
    console.log(output);
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
