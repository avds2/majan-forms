import { createRequire } from 'node:module';
import { readFile } from 'node:fs/promises';
import { firefox, webkit } from 'playwright';

const require = createRequire(import.meta.url);
const axePath = require.resolve('axe-core/axe.min.js');
const axeSource = await readFile(axePath, 'utf8');
const baseURL = process.env.FORMS_BROWSER_BASE_URL;
const username = process.env.FORMS_BROWSER_USERNAME;
const password = process.env.FORMS_BROWSER_PASSWORD;

if (!baseURL || !username || !password) {
  throw new Error('FORMS_BROWSER_BASE_URL, FORMS_BROWSER_USERNAME and FORMS_BROWSER_PASSWORD are required');
}

const browserTypes = [
  ['firefox', firefox],
  ['webkit', webkit]
];

for (const [browserName, browserType] of browserTypes) {
  const browser = await browserType.launch({ headless: true });
  try {
    const context = await browser.newContext({ locale: 'fa-IR', viewport: { width: 1440, height: 900 } });
    const page = await context.newPage();
    const runtimeErrors = [];
    let authenticated = false;
    page.on('console', (message) => {
      if (message.type() !== 'error') return;
      if (!authenticated && message.text().includes('401 (Unauthorized)')) return;
      runtimeErrors.push(`console: ${message.text()}`);
    });
    page.on('pageerror', (error) => runtimeErrors.push(`page: ${error.message}`));

    await page.goto(baseURL, { waitUntil: 'networkidle' });
    await page.getByLabel('ایمیل یا نام کاربری').fill(username);
    await page.getByLabel('رمز عبور').fill(password);
    await page.getByRole('button', { name: 'ورود به پنل' }).click();
    await page.getByRole('heading', { name: 'پروژه‌ها', exact: true }).waitFor();
    authenticated = true;

    const project = await page.evaluate(async () => {
      const response = await fetch('/api/projects', { credentials: 'same-origin' });
      const payload = await response.json();
      return payload.data?.[0];
    });
    if (!project?.id || !project?.slug) throw new Error(`${browserName}: no project fixture is available`);

    const routes = [
      '/',
      '/users',
      '/trash',
      '/system',
      ...['overview', 'builder', 'routes', 'publish', 'responses', 'reports', 'ai', 'settings'].map(
        (section) => `/projects/${encodeURIComponent(project.id)}/${section}`
      )
    ];
    for (const route of routes) {
      await page.goto(`${baseURL}${route}`, { waitUntil: 'networkidle' });
      await page.waitForTimeout(150);
      const pageState = await page.evaluate(() => ({
        overflow: document.documentElement.scrollWidth - window.innerWidth,
        unavailable: document.body.innerText.includes('پنل در دسترس نیست')
      }));
      if (pageState.unavailable) throw new Error(`${browserName} ${route}: panel unavailable`);
      if (pageState.overflow > 1) throw new Error(`${browserName} ${route}: horizontal overflow ${pageState.overflow}px`);

      await page.evaluate(axeSource);
      const violations = await page.evaluate(async () => {
        const result = await globalThis.axe.run(document, {
          resultTypes: ['violations'],
          runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] }
        });
        return result.violations
          .filter((violation) => violation.impact === 'critical' || violation.impact === 'serious')
          .map((violation) => ({
            id: violation.id,
            impact: violation.impact,
            help: violation.help,
            targets: violation.nodes.slice(0, 5).map((node) => node.target)
          }));
      });
      if (violations.length > 0) {
        throw new Error(`${browserName} ${route}: accessibility ${JSON.stringify(violations)}`);
      }
    }

    const mobile = await context.newPage();
    await mobile.setViewportSize({ width: 390, height: 844 });
    for (const route of ['/', '/system', `/projects/${encodeURIComponent(project.id)}/builder`]) {
      await mobile.goto(`${baseURL}${route}`, { waitUntil: 'networkidle' });
      await mobile.waitForTimeout(150);
      const overflow = await mobile.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      if (overflow > 1) throw new Error(`${browserName} mobile ${route}: horizontal overflow ${overflow}px`);
    }

    const publicPage = await context.newPage();
    await publicPage.goto(`${baseURL}/f/${encodeURIComponent(project.slug)}`, { waitUntil: 'networkidle' });
    await publicPage.evaluate(axeSource);
    const publicViolations = await publicPage.evaluate(async () => {
      const result = await globalThis.axe.run(document, {
        resultTypes: ['violations'],
        runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] }
      });
      return result.violations
        .filter((violation) => violation.impact === 'critical' || violation.impact === 'serious')
        .map((violation) => ({
          id: violation.id,
          impact: violation.impact,
          help: violation.help,
          targets: violation.nodes.slice(0, 5).map((node) => node.target)
        }));
    });
    if (publicViolations.length > 0) {
      throw new Error(`${browserName} public form: accessibility ${JSON.stringify(publicViolations)}`);
    }
    if (runtimeErrors.length > 0) throw new Error(`${browserName}: ${runtimeErrors.join(' | ')}`);
    console.log(`${browserName}: desktop, mobile, public form and accessibility passed`);
  } finally {
    await browser.close();
  }
}
