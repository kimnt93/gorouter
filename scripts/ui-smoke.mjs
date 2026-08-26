import fs from 'node:fs/promises'
import puppeteer from 'puppeteer-core'

const baseURL = process.env.UI_BASE_URL ?? 'http://127.0.0.1:8090'
const accessKey = process.env.MASTER_KEY
const screenshotDir = process.env.UI_SCREENSHOT_DIR ?? '/tmp/gorouter-ui-smoke'

if (!accessKey) throw new Error('MASTER_KEY is required for the authenticated UI smoke test')
await fs.mkdir(screenshotDir, { recursive: true })

const browser = await puppeteer.launch({ executablePath: process.env.CHROME_BIN ?? '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] })
const page = await browser.newPage()
const results = []
const smokeConnectionName = `ui-smoke-${Date.now()}`
page.on('pageerror', (error) => process.stderr.write(`browser page error: ${error.message}\n`))

async function assertLayout(name, path, viewport) {
  await page.setViewport(viewport)
  await page.goto(`${baseURL}${path}`, { waitUntil: 'networkidle0' })
  if (page.url().includes('/login')) {
    await page.type('input[name="key"]', accessKey)
    await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.click('button')])
    if (path !== '/dashboard/analysis' && path !== '/') await page.goto(`${baseURL}${path}`, { waitUntil: 'networkidle0' })
  }
  await page.waitForSelector('.app-shell')
  await page.screenshot({ path: `${screenshotDir}/${name}.png`, fullPage: true })
  const dimensions = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    viewportWidth: document.documentElement.clientWidth,
    offenders: [...document.querySelectorAll('body *')].filter((element) => element.getBoundingClientRect().right > document.documentElement.clientWidth + 1).slice(0, 5).map((element) => `${element.tagName.toLowerCase()}.${element.className}`),
  }))
  if (dimensions.documentWidth > dimensions.viewportWidth) throw new Error(`${name} overflows horizontally: ${dimensions.documentWidth} > ${dimensions.viewportWidth}; ${dimensions.offenders.join(', ')}`)
  results.push({ name, path, ...dimensions })
}

try {
  await assertLayout('analysis-desktop', '/dashboard/analysis', { width: 1440, height: 900 })
  await assertLayout('logs-desktop', '/dashboard/logs', { width: 1440, height: 900 })
  const order = await page.$eval('.token-order', (element) => element.textContent ?? '')
  if (!order.includes('[in / out / cache read / cache write]')) throw new Error('token order legend is missing')
  const row = await page.$('.logs-table tbody tr')
  if (row) {
    await row.click()
    await page.waitForSelector('[role="dialog"]')
    const privacy = await page.$eval('.safe-note', (element) => element.textContent ?? '')
    if (!privacy.includes('excludes prompts')) throw new Error('safe modal boundary is missing')
    await page.keyboard.press('Escape')
  }
  await assertLayout('cache-desktop', '/dashboard/cache', { width: 1440, height: 900 })
  const cacheCopy = await page.$eval('.page-header p', (element) => element.textContent ?? '')
  if (!cacheCopy.includes('upstream providers')) throw new Error('provider-side cache attribution is missing')
  await assertLayout('providers-desktop', '/dashboard/providers', { width: 1440, height: 900 })
  const providerCards = await page.$$('.provider-card-react')
  if (providerCards.length < 2) throw new Error('provider catalog did not render')
  const apiCard = await page.evaluateHandle(() => [...document.querySelectorAll('.provider-card-react')].find((card) => card.textContent?.includes('OpenAI-compatible')))
  const apiConnect = await apiCard.asElement()?.$('.connect-button')
  if (!apiConnect) throw new Error('API-key provider connection action is missing')
  await apiConnect.click()
  await page.waitForSelector('[role="dialog"] input[type="password"]')
  const dialogInputs = await page.$$('[role="dialog"] input')
  await dialogInputs[0].click({ clickCount: 3 }); await dialogInputs[0].type(smokeConnectionName)
  const baseURLInput = await page.$('[role="dialog"] input[type="url"]')
  if (baseURLInput) { await baseURLInput.click({ clickCount: 3 }); await baseURLInput.type('https://example.invalid/v1') }
  const secretInput = await page.$('[role="dialog"] input[type="password"]')
  await secretInput.type('ui-smoke-not-a-real-secret')
  await page.click('[role="dialog"] .dialog-actions .button')
  await page.waitForSelector('[role="dialog"]', { hidden: true })
  await page.waitForFunction((name) => document.body.textContent?.includes(name), {}, smokeConnectionName)
  await assertLayout('connections-desktop', '/dashboard/credentials', { width: 1440, height: 900 })
  await assertLayout('models-desktop', '/dashboard/models', { width: 1440, height: 900 })
  await page.click('.page-header .button')
  await page.waitForSelector('[role="dialog"] .route-editor-row')
  await page.click('[role="dialog"] .form-section-heading .button')
  const routeRows = await page.$$('[role="dialog"] .route-editor-row')
  if (routeRows.length !== 2) throw new Error('multi-route model editor did not add a route')
  await page.keyboard.press('Escape')
  await assertLayout('users-desktop', '/dashboard/users', { width: 1440, height: 900 })
  await assertLayout('organizations-desktop', '/dashboard/organizations', { width: 1440, height: 900 })
  await assertLayout('keys-desktop', '/dashboard/keys', { width: 1440, height: 900 })
  await assertLayout('audit-desktop', '/dashboard/audit', { width: 1440, height: 900 })
  await assertLayout('analysis-mobile', '/dashboard/analysis', { width: 375, height: 812 })
  await assertLayout('logs-mobile', '/dashboard/logs', { width: 375, height: 812 })
  await assertLayout('cache-mobile', '/dashboard/cache', { width: 375, height: 812 })
  await assertLayout('providers-mobile', '/dashboard/providers', { width: 375, height: 812 })
  await assertLayout('keys-mobile', '/dashboard/keys', { width: 375, height: 812 })
  const activeNavigationVisible = await page.$eval('.sidebar nav', (navigation) => {
    const active = navigation.querySelector('.nav-link.active')
    if (!active) return false
    const navigationBounds = navigation.getBoundingClientRect()
    const activeBounds = active.getBoundingClientRect()
    return activeBounds.left >= navigationBounds.left && activeBounds.right <= navigationBounds.right
  })
  if (!activeNavigationVisible) throw new Error('active mobile navigation item is outside the visible navigation area')
  process.stdout.write(`${JSON.stringify({ ok: true, screenshots: screenshotDir, results }, null, 2)}\n`)
} finally {
  await page.evaluate(async (name) => {
    const response = await fetch('/admin/credentials', { credentials: 'include' })
    if (!response.ok) return
    const credentials = await response.json()
    if (!Array.isArray(credentials)) return
    for (const credential of credentials) if (credential.name === name) await fetch(`/admin/credentials/${encodeURIComponent(credential.id)}`, { method: 'DELETE', credentials: 'include' })
  }, smokeConnectionName).catch(() => undefined)
  await browser.close()
}
