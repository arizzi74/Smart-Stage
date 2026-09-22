# Public website and repository discovery

The static website lives in `docs/site`. It reuses the real sample-show screenshots
from `docs/screenshots` and the existing application icon. The website is live at
<https://arizzi74.github.io/Smart-Stage/>.

English and Italian are published as static pages at `/Smart-Stage/en/` and
`/Smart-Stage/it/`. The main address selects the first supported language in the
browser's preference list, including regional variants such as `it-CH`. If none
match, it uses English. A choice made with **EN / IT** takes precedence on later
visits and is saved only in local browser storage. Direct language links always
keep their language; changing language preserves the current section and query
parameters. With JavaScript disabled, the main address offers both language links.

Translate website content in `docs/site/en/index.html` and
`docs/site/it/index.html`, and the overview in `docs/site/index.md` (English) and
`docs/site/it/index.md` (Italian). App screenshots retain the real English app
interface. Language routing and translated clipboard messages live in `site.js`.

## Publication and remaining account settings

Pages now uses GitHub Actions. The first public deployment
[completed successfully](https://github.com/arizzi74/Smart-Stage/actions/runs/35686056215)
on September 22, 2026 at 04:13:39 UTC; its deploy job took nine seconds. The
homepage and public assets have been verified over HTTPS.

Changes to website sources, screenshots or icons publish automatically. To run
publication manually, open [Publish Smart Stage website](https://github.com/arizzi74/Smart-Stage/actions/workflows/site.yml)
and select **Run workflow** on `main`. If the workflow graph keeps showing an old
running timer, refresh it and check the individual job or the public site.

GitHub rejected repository metadata writes with HTTP 403 using the current
automation credential. A repository administrator can finish these account settings
while signed in as `arizzi74`:

1. On the [repository home page](https://github.com/arizzi74/Smart-Stage), use the
   gear beside **About** to set the description, website and topics below.
2. In [repository settings](https://github.com/arizzi74/Smart-Stage/settings),
   upload [`site/assets/social-preview.png`](site/assets/social-preview.png)
   as the **Social preview** image.

## Repository metadata

Description:

> Live show control for macOS and Windows: play audio, video and image cues from your phone or tablet, with a dedicated stage display.

Website: `https://arizzi74.github.io/Smart-Stage/`

Topics:

```text
show-control live-performance cue-playback audio-playback video-playback
stage-display remote-control macos windows golang self-hosted
```

## Build and preview

From the repository root:

```sh
python3 scripts/build-site.py
python3 -m http.server 8080 --bind 127.0.0.1 --directory dist/site
```

Open <http://127.0.0.1:8080/>. The build uses Python's standard library and copies
only the public site and selected artwork into `dist/site`; the app's private
Admin interface and verification archives are not website assets.

To regenerate the social card with the existing browser development tools:

```sh
npm ci --prefix scripts/browser --ignore-scripts --no-audit --no-fund
node scripts/browser/node_modules/playwright/cli.js install chromium
node scripts/site/render-social-preview.cjs
node scripts/site/render-social-preview.cjs it
```

The HTML sources are `scripts/site/social-preview.html` and
`scripts/site/social-preview-it.html`. Generated 1280 × 640 PNGs are committed and
reused by each language's social metadata. The site itself has no runtime
dependencies. CI uses the browser tools to verify language selection, preference
persistence, direct links, clipboard messages and operation without JavaScript.
Run those checks locally with `node scripts/site/check-locales.cjs` after building.

## Search and documentation

The site has a descriptive title and meta description, a canonical URL, social
preview metadata, factual `SoftwareApplication` structured data, an XML sitemap,
and alternate Markdown overviews. Both language pages have self-canonical URLs,
reciprocal `hreflang` links, and an `x-default` link to the main address; all three
addresses appear in the sitemap. Keep metadata aligned with visible page content.
`llms.txt` is a small documentation index, not a promise of search ranking or AI
recommendations. Keep the repository-root and site copies identical.

The owner can verify the site's URL prefix in Google Search
Console and submit `https://arizzi74.github.io/Smart-Stage/sitemap.xml`. Verification
requires the owner's Google account; no verification token has been invented or
added. Search engines decide when to crawl and index the site.

No project-level `robots.txt` is needed: crawlers read that file at the hostname
root (`https://arizzi74.github.io/robots.txt`), not under `/Smart-Stage/`. A project
file there would not control the host. This site has no indexing restrictions.

No project license was selected as part of these changes. The site does not claim
an open-source license. Any license grant should be an explicit owner decision.

References: [GitHub Pages workflows](https://docs.github.com/en/pages/getting-started-with-github-pages/using-custom-workflows-with-github-pages),
[GitHub topics](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/classifying-your-repository-with-topics),
[Google SEO guidance](https://developers.google.com/search/docs/fundamentals/seo-starter-guide),
[Google AI search guidance](https://developers.google.com/search/docs/appearance/ai-features),
[llms.txt proposal](https://llmstxt.org/).
