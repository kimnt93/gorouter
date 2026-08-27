import puppeteer from 'puppeteer-core'

const baseURL = process.env.UI_BASE_URL ?? 'http://127.0.0.1:8090'
const accessKey = process.env.UI_ACCESS_KEY
if (!accessKey) throw new Error('UI_ACCESS_KEY is required')
const browser = await puppeteer.launch({ executablePath: process.env.CHROME_BIN ?? '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] })
const page = await browser.newPage()
await page.setViewport({ width: 1440, height: 900 })
try {
  await page.goto(`${baseURL}/dashboard/analysis`, { waitUntil: 'networkidle0' })
  if (page.url().includes('/login')) {
    await page.type('input[name="key"]', accessKey)
    await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.click('button')])
  }
  await page.waitForSelector('.app-shell')
  if (await page.$('.context-switcher')) throw new Error('ordinary member can access the View As switcher')
  if (await page.$('.view-as-notice')) throw new Error('ordinary member is presented as an organization administrator')
  if (await page.$('.nav-link[href^="/dashboard/users"]')) throw new Error('ordinary member can access master-only users navigation')
  if ((await page.$eval('body', (element) => element.textContent ?? '')).includes('Master view')) throw new Error('ordinary member can access Master view')
  process.stdout.write(`${JSON.stringify({ ok: true, base_url: baseURL, view_as_hidden: true, master_view_hidden: true }, null, 2)}\n`)
} finally {
  await browser.close()
}
