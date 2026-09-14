// Run only against an explicitly selected disposable Router with synthetic usage.
import puppeteer from 'puppeteer-core'
const base = process.env.UI_BASE_URL
const key = process.env.UI_ACCESS_KEY
if (!base || !key) throw new Error('UI_BASE_URL and UI_ACCESS_KEY are required')
const browser = await puppeteer.launch({ executablePath: process.env.CHROME_BIN ?? '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] })
try {
  const page = await browser.newPage()
  await page.setViewport({ width: 1440, height: 900 })
  const errors = []
  page.on('pageerror', () => errors.push('page_error'))
  await page.goto(`${base}/login`, { waitUntil: 'networkidle0' })
  await page.type('input[name="key"]', key)
  await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.click('form button')])
  await page.goto(`${base}/dashboard/analysis`, { waitUntil: 'networkidle0' })
  await page.waitForFunction(() => document.body.textContent.includes('Stored usage · coverage unknown'))
  await page.waitForSelector('.accounting-report-controls')
  const snapshot = await page.evaluate(() => ({ overflow: document.documentElement.scrollWidth > innerWidth, charts: document.querySelectorAll('.vertical-chart').length, groups: document.body.textContent.includes('smoke-alpha') }))
  if (snapshot.overflow || snapshot.charts !== 2 || !snapshot.groups || errors.length) throw new Error('Accounting layout/content assertion failed')
  await page.click('.accounting-report-controls .multi-picker summary')
  await page.type('input[aria-label="Search agents"]', 'smoke-alpha')
  const response = page.waitForResponse((res) => res.url().includes('/admin/usage/report?') && res.url().includes('agent_id=smoke-alpha'))
  await page.click('input[type="checkbox"][aria-label="smoke-alpha"]')
  const report = await response
  if (!report.ok()) throw new Error('Agent filter failed')
  await page.waitForSelector('button[aria-label="Remove smoke-alpha"]')
  await page.screenshot({ path: '/tmp/gorouter-accounting-ui.png', fullPage: true })
  process.stdout.write(JSON.stringify({ ok: true, viewport: '1440x900', grouped_charts: 2, searchable_agent_filter: true, overflow: false, page_errors: errors.length }) + '\n')
} finally { await browser.close() }
