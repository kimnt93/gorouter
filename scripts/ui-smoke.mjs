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
  await assertLayout('analysis-mobile', '/dashboard/analysis', { width: 375, height: 812 })
  await assertLayout('logs-mobile', '/dashboard/logs', { width: 375, height: 812 })
  await assertLayout('cache-mobile', '/dashboard/cache', { width: 375, height: 812 })
  process.stdout.write(`${JSON.stringify({ ok: true, screenshots: screenshotDir, results }, null, 2)}\n`)
} finally {
  await browser.close()
}
