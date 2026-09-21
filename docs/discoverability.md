# Public website and repository discovery

The static website lives in `docs/site`. It reuses the real sample-show screenshots
from `docs/screenshots` and the existing application icon. The public address,
once Pages is enabled, is <https://arizzi74.github.io/Smart-Stage/>.

## Enable publication

The current automation credential can push commits but GitHub rejected both
repository-settings changes and Pages activation with HTTP 403. The site and
workflow are ready; a repository administrator can finish these account settings:

1. Open [Settings → Pages](https://github.com/arizzi74/Smart-Stage/settings/pages)
   and set **Source → GitHub Actions**.
2. Open [Publish Smart Stage website](https://github.com/arizzi74/Smart-Stage/actions/workflows/site.yml)
   and select **Run workflow** on `main`.
3. On the [repository home page](https://github.com/arizzi74/Smart-Stage), use the
   gear beside **About** to set the description, website and topics below.
4. In [repository settings](https://github.com/arizzi74/Smart-Stage/settings),
   upload [`site/assets/social-preview.png`](site/assets/social-preview.png)
   as the **Social preview** image.

Subsequent changes to website sources, screenshots or icons publish automatically.
Until Pages is enabled, the workflow builds the complete website artifact and
explicitly skips deployment. A successful build alone does not mean the site is live.

## Repository metadata

Description:

> Live show control for macOS and Windows: play audio, video and image cues from your phone or tablet, with a dedicated stage display.

Website (after publication): `https://arizzi74.github.io/Smart-Stage/`

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
```

Its HTML source is `scripts/site/social-preview.html`. The generated 1280 × 640
PNG is committed and reused by the website's social metadata; deploying the site
does not require Node or a browser.

## Search and documentation

The site has a descriptive title and meta description, a canonical URL, social
preview metadata, factual `SoftwareApplication` structured data, an XML sitemap,
and an alternate Markdown overview. Keep those aligned with visible page content.
`llms.txt` is a small documentation index, not a promise of search ranking or AI
recommendations. Keep the repository-root and site copies identical.

After publication, the owner can verify the site's URL prefix in Google Search
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
