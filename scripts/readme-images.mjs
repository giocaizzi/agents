// Frame terminal captures for README matching Shortty style (gradient, padding, drop shadows)
import { writeFileSync, existsSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const home = os.homedir();
const CHROMIUM_PATH = path.join(home, 'Library/Caches/ms-playwright/chromium_headless_shell-1254/chrome-headless-shell-mac-arm64/chrome-headless-shell');
const OUT_FILE = path.resolve('docs/assets/showcase.png');

async function render() {
  let playwright;
  try {
    playwright = await import(path.join(home, 'Documents/github/shortty/node_modules/playwright/index.mjs'));
  } catch {
    console.log('Playwright not available locally, skipping image re-render.');
    return;
  }

  const { chromium } = playwright;
  const browser = await chromium.launch({
    executablePath: existsSync(CHROMIUM_PATH) ? CHROMIUM_PATH : undefined,
    args: ['--no-sandbox'],
  });

  const page = await browser.newPage({
    viewport: { width: 1400, height: 950 },
    deviceScaleFactor: 2,
  });

  const html = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 100vh;
    background: linear-gradient(135deg, #8ec5fc 0%, #a5b4fc 50%, #d8b4fe 100%);
    padding: 80px 100px;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  }
  .window {
    width: 1080px;
    background: #1e1e2e;
    border-radius: 18px;
    box-shadow: 0 40px 100px rgba(20, 16, 60, 0.45), 0 4px 16px rgba(20, 16, 60, 0.25);
    border: 1px solid rgba(255, 255, 255, 0.16);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  .titlebar {
    height: 44px;
    background: #181825;
    display: flex;
    align-items: center;
    padding: 0 18px;
    position: relative;
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  }
  .buttons {
    display: flex;
    gap: 8px;
  }
  .dot {
    width: 12px;
    height: 12px;
    border-radius: 50%;
  }
  .dot.close { background: #ff5f56; border: 1px solid #e0443e; }
  .dot.min { background: #ffbd2e; border: 1px solid #dea123; }
  .dot.max { background: #27c93f; border: 1px solid #1aab29; }
  .title {
    position: absolute;
    left: 0;
    right: 0;
    text-align: center;
    color: #a6adc8;
    font-size: 13px;
    font-weight: 500;
    letter-spacing: -0.01em;
    pointer-events: none;
  }
  .terminal {
    padding: 24px 28px;
    font-family: "JetBrainsMono Nerd Font", "JetBrains Mono", "SF Mono", Menlo, Consolas, monospace;
    font-size: 13.5px;
    line-height: 1.55;
    color: #cdd6f4;
  }
  .prompt-line {
    margin-bottom: 12px;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .user { color: #cba6f7; font-weight: 600; }
  .dir { color: #89b4fa; }
  .cmd { color: #f9e2af; font-weight: 600; }
  pre {
    font-family: inherit;
    font-size: inherit;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .statusline {
    margin-top: 24px;
    padding-top: 14px;
    border-top: 1px solid #313244;
    display: flex;
    gap: 12px;
    font-size: 12.5px;
  }
  .pill {
    background: #313244;
    padding: 3px 10px;
    border-radius: 6px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .c-blue { color: #89b4fa; }
  .c-mauve { color: #cba6f7; }
  .c-green { color: #a6e3a1; }
  .c-yellow { color: #f9e2af; }
  .c-peach { color: #fab387; }
  .c-red { color: #f38ba8; }
  .c-overlay { color: #6c7086; }
</style>
</head>
<body>
  <div class="window" id="capture">
    <div class="titlebar">
      <div class="buttons">
        <div class="dot close"></div>
        <div class="dot min"></div>
        <div class="dot max"></div>
      </div>
      <div class="title">agents — doctor & usage</div>
    </div>
    <div class="terminal">
      <div class="prompt-line">
        <span class="user">user@macbook</span>
        <span class="dir">~</span>
        <span style="color:#6c7086;">$</span>
        <span class="cmd">agents usage -live && agents doctor</span>
      </div>
      <pre><span class="c-blue">agents usage — subscription usage per provider account</span>   <span class="c-overlay">(live)</span>

<span class="c-mauve">anthropic</span>       Max 20x · user@example.com      <span class="c-overlay">claude</span>
  7d                  <span class="c-green">████████████████████</span>  100%   <span class="c-yellow">resets in 2d 18h</span>
<span class="c-mauve">github-copilot</span>  Pro · octocat                   <span class="c-overlay">copilot, opencode, pi</span>
  chat                <span class="c-overlay">░░░░░░░░░░░░░░░░░░░░</span>    1%   197 of 200 left · resets Dec 1
  completions         <span class="c-overlay">░░░░░░░░░░░░░░░░░░░░</span>    0%   2000 of 2000 left · resets Dec 1
<span class="c-mauve">google</span>          Antigravity                     <span class="c-overlay">antigravity</span>
  Gemini · 7d         <span class="c-blue">███░░░░░░░░░░░░░░░░░</span>   13%   resets Sun 12:25
  Gemini · 5h         <span class="c-yellow">█████████░░░░░░░░░░░</span>   47%   resets in 2h 30m
<span class="c-mauve">openai</span>          Plus · team@company.com         <span class="c-overlay">codex, opencode</span>
  5h                  <span class="c-overlay">░░░░░░░░░░░░░░░░░░░░</span>    0%   resets in 4h 30m
  7d                  <span class="c-overlay">░░░░░░░░░░░░░░░░░░░░</span>    0%   resets Sun 14:20

<span class="c-blue">agents doctor — models, accounts and capacity</span>

<span class="c-green">↑</span> <span class="c-yellow">scout</span>    GPT-5.5 → Gemini 2.5 Flash     <span class="c-overlay">better-ranked · #23 → #18 · flat plan · copilot, pi</span>
<span class="c-green">↑</span> <span class="c-yellow">worker</span>   GPT-5.5 → Claude 3.5 Haiku     <span class="c-overlay">better-ranked · #23 → #12 · flat plan · copilot, pi</span>
<span class="c-green">↑</span> <span class="c-yellow">driver</span>   GPT-5.5 → Claude 3.7 Sonnet    <span class="c-overlay">better-ranked · #6 → #1 · flat plan · copilot, pi</span></pre>
      <div class="statusline">
        <div class="pill"><span class="c-green">●</span> <span class="c-mauve">claude-3-7-sonnet</span></div>
        <div class="pill"><span class="c-yellow">󰚥</span> <span class="c-yellow">100% left</span></div>
        <div class="pill"><span class="c-blue">󰊢</span> <span class="c-blue">main</span></div>
        <div class="pill"><span class="c-overlay"></span> <span class="c-overlay">~/workspace</span></div>
      </div>
    </div>
  </div>
</body>
</html>`;

  await page.setContent(html);
  await page.waitForTimeout(300);
  const element = await page.$('body');
  const buffer = await element.screenshot();
  writeFileSync(OUT_FILE, buffer);
  await browser.close();
  console.log(`Rendered: ${OUT_FILE}`);
}

render().catch(console.error);
