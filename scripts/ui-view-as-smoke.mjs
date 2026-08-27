import fs from 'node:fs/promises'
import puppeteer from 'puppeteer-core'

const baseURL = process.env.UI_BASE_URL ?? 'http://127.0.0.1:8090'
const accessKey = process.env.UI_ACCESS_KEY ?? process.env.MASTER_KEY
const organizationID = process.env.UI_ORGANIZATION_ID
const expectMaster = process.env.UI_EXPECT_MASTER !== '0'
const screenshotDir = process.env.UI_SCREENSHOT_DIR ?? '/tmp/gorouter-ui-view-as'
if (!accessKey || !organizationID) throw new Error('MASTER_KEY and UI_ORGANIZATION_ID are required')
await fs.mkdir(screenshotDir, { recursive: true })

const browser = await puppeteer.launch({ executablePath: process.env.CHROME_BIN ?? '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] })
const page = await browser.newPage()
await page.setViewport({ width: 1440, height: 900 })
try {
  await page.goto(`${baseURL}/dashboard/analysis?organization_id=${encodeURIComponent(organizationID)}`, { waitUntil: 'networkidle0' })
  if (page.url().includes('/login')) {
    await page.type('input[name="key"]', accessKey)
    await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.click('button')])
    await page.goto(`${baseURL}/dashboard/analysis?organization_id=${encodeURIComponent(organizationID)}`, { waitUntil: 'networkidle0' })
  }
  await page.waitForSelector('.view-as-notice')
  const notice = await page.$eval('.view-as-notice', (element) => element.textContent ?? '')
  if (!notice.includes('Viewing as organization admin')) throw new Error('View As notice is missing')
  if (await page.$('.nav-link[href^="/dashboard/users"]')) throw new Error('master-only Users navigation remains visible in organization View As mode')

  await page.click('.context-switcher .searchable-select-trigger')
  await page.waitForSelector('.context-switcher .select-search input')
  const firstNumber = await page.$eval('.context-switcher .searchable-select-list button b', (element) => element.textContent ?? '')
  if (firstNumber !== '01') throw new Error('View As options are not numbered')
  await page.type('.context-switcher .select-search input', 'Microsoft')
  const optionCount = await page.$$eval('.context-switcher .searchable-select-list button', (items) => items.length)
  if (optionCount < 1) throw new Error('View As organization search returned no results')
  if (!expectMaster && await page.$eval('.context-switcher', (element) => element.textContent?.includes('Master view') ?? false)) throw new Error('organization admin was offered Master view')
  await page.screenshot({ path: `${screenshotDir}/view-as-dropdown.png` })
  await page.keyboard.press('Escape')

  const firstColumn = await page.$('.vertical-column')
  let tooltip = null
  if (firstColumn) {
    await firstColumn.hover()
    await page.waitForSelector('.vertical-column .chart-tooltip', { visible: true })
    tooltip = await page.$eval('.vertical-column .chart-tooltip', (element) => { const box = element.getBoundingClientRect(); const panel = element.closest('.panel')?.getBoundingClientRect(); return { left: box.left, right: box.right, height: box.height, panelLeft: panel?.left ?? 0, panelRight: panel?.right ?? document.documentElement.clientWidth } })
    if (tooltip.left < tooltip.panelLeft || tooltip.right > tooltip.panelRight || tooltip.height > 301) throw new Error(`chart tooltip is outside its bounded layout: ${JSON.stringify(tooltip)}`)
  }
  await page.screenshot({ path: `${screenshotDir}/analysis-view-as.png`, fullPage: true })

  await page.goto(`${baseURL}/dashboard/organizations?organization_id=${encodeURIComponent(organizationID)}`, { waitUntil: 'networkidle0' })
  const organizationCards = await page.$$('.organization-card')
  if (organizationCards.length !== 1) throw new Error(`expected one organization card in View As mode, got ${organizationCards.length}`)
  const memberTotal = await page.$eval('.organization-member-total', (element) => element.textContent ?? '')
  if (!memberTotal.includes('Total user members')) throw new Error('organization member total is missing')
  await page.screenshot({ path: `${screenshotDir}/organizations-view-as.png`, fullPage: true })

  const scoped = await page.evaluate(async (id) => {
    const [keys, credentials, models] = await Promise.all([
      fetch(`/admin/api-keys?limit=500&organization_id=${encodeURIComponent(id)}`).then((response) => response.json()),
      fetch(`/admin/credentials?organization_id=${encodeURIComponent(id)}`).then((response) => response.json()),
      fetch(`/admin/models?organization_id=${encodeURIComponent(id)}`).then((response) => response.json()),
    ])
    return { keys: keys.data?.length ?? 0, keysScoped: (keys.data ?? []).every((key) => key.context_organization_id === id), credentials: credentials.length ?? 0, credentialsScoped: credentials.every((credential) => credential.owner_tenant_id === id), models: models.length ?? 0, modelsScoped: models.every((model) => model.routes?.length > 0) }
  }, organizationID)
  if (!scoped.keysScoped || !scoped.credentialsScoped || !scoped.modelsScoped) throw new Error(`View As API scoping failed: ${JSON.stringify(scoped)}`)

  if (expectMaster) {
    await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.click('.view-as-notice button')])
    if (new URL(page.url()).searchParams.has('organization_id')) throw new Error('Return to Master view did not clear organization context')
  } else if (await page.$('.view-as-notice button')) throw new Error('organization admin can return to Master view')
  process.stdout.write(`${JSON.stringify({ ok: true, principal: expectMaster ? 'master' : 'organization_admin', organization_id: organizationID, tooltip, member_total: memberTotal.trim(), scoped, screenshots: screenshotDir }, null, 2)}\n`)
} finally {
  await browser.close()
}
