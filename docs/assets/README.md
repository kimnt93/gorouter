# GoRouter artwork

The GoRouter mascot is an original vector drawing inspired by the Go gopher
created by [Renée French](https://go.dev/blog/gopher). It is a project mascot,
not an official Go logo or an endorsement by the Go team.

- **Gopher:** cyan body, round ears, oversized eyes, and buck teeth.
- **Routing:** a handheld network router and directional, branching paths.
- **Multi-tenancy:** three enclosed team badges with violet, mint, and amber
  connections and matching router ports. Each team has its own boundary.

`gorouter-logo.svg` is the editable, transparent README artwork. The simplified
`../../public/assets/favicon.svg` keeps the face and three ports legible at tab
sizes. Both use self-contained shapes, with no fonts or external image requests.

To regenerate the 16, 32, and 48 px ICO fallback after editing the favicon:

```sh
npm ci
CHROME_BIN=/path/to/chrome npm run build:branding
npm run build
```

The renderer uses the existing Puppeteer dependency and defaults to
`/usr/bin/google-chrome`. Vite copies the public favicons into the committed Go
SPA bundle. Dashboard and server-rendered login pages share those assets.
