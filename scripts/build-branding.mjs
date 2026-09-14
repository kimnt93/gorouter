// Render the editable SVG favicon into a multi-resolution ICO. No image API is used.
import fs from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import puppeteer from 'puppeteer-core'

const source = new URL('../public/assets/favicon.svg', import.meta.url)
const output = new URL('../public/assets/favicon.ico', import.meta.url)
const sizes = [16, 32, 48]
const browser = await puppeteer.launch({
  executablePath: process.env.CHROME_BIN ?? '/usr/bin/google-chrome',
  headless: true,
  args: ['--no-sandbox', '--disable-dev-shm-usage'],
})

try {
  const page = await browser.newPage()
  const svg = await fs.readFile(source, 'utf8')
  const images = []
  for (const size of sizes) {
    await page.setViewport({ width: size, height: size, deviceScaleFactor: 1 })
    await page.setContent(`<style>html,body{margin:0;background:transparent}svg{display:block;width:100vw;height:100vh}</style>${svg}`)
    images.push(Buffer.from(await page.screenshot({ type: 'png', omitBackground: true })))
  }
  // ICO directory entries point to PNG images with alpha at each native tab size.
  const header = Buffer.alloc(6 + images.length * 16)
  header.writeUInt16LE(1, 2)
  header.writeUInt16LE(images.length, 4)
  let offset = header.length
  images.forEach((image, index) => {
    const entry = 6 + index * 16
    header[entry] = sizes[index]
    header[entry + 1] = sizes[index]
    header.writeUInt16LE(1, entry + 4)
    header.writeUInt16LE(32, entry + 6)
    header.writeUInt32LE(image.length, entry + 8)
    header.writeUInt32LE(offset, entry + 12)
    offset += image.length
  })
  await fs.writeFile(output, Buffer.concat([header, ...images]))
  console.log(`Generated ${fileURLToPath(output)} (${sizes.join(', ')} px)`)
} finally {
  await browser.close()
}
