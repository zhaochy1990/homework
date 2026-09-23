# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

WeChat Mini Program written in TypeScript, based on the official `miniprogram-ts-quickstart` template. There is no CLI build/lint/test tooling — compilation, preview, and upload are done through **WeChat DevTools (微信开发者工具)**, which reads `miniprogram/project.config.json`. The DevTools TypeScript compiler plugin compiles `.ts` files automatically; do not write compiled `.js` files by hand.

## Structure

- `project.config.json` (repo root of this dir) — DevTools config; `miniprogramRoot`/`srcMiniprogramRoot` point to the inner `miniprogram/` directory. `appid` is configured here.
- `miniprogram/` — actual source root:
  - `app.ts` / `app.json` / `app.wxss` — app entry. New pages must be registered in `app.json` `pages` array and the four bottom tabs in `tabBar`. `app.wxss` holds the global flat design tokens (CSS variables on `page`).
  - `config.ts` — auth-service / backend base URLs and the public client id.
  - `api/` — `auth.ts` (token_exchange login + one-time refresh), `request.ts` (base URL, Bearer, 401 refresh+replay, error shape, loading), `user.ts`, `error.ts`.
  - `pages/today|growth|library|mine` — the four tabs; each page is a 4-file set (`.ts` logic, `.wxml`, `.wxss`, `.json`).
  - `components/` — `subject-card` (分科作业卡) and `todo-row` (勾选行).
  - `utils/async.ts` — `singleFlight` (并发去重).
  - `typings/` — ambient types, including `IAppOption` defined in `typings/index.d.ts`.
- `scripts/selfcheck.ts` — plain `assert` self-check for `utils/async`; run `npm run selfcheck` (Node ≥ 22.6 strips types natively). Excluded from `tsconfig.json`.
- `tsconfig.json` — strict mode is fully enabled (`strict`, `noUnusedLocals`, `noUnusedParameters`, etc.), so unused variables/params will fail compilation. Run `npm run typecheck`.

## Conventions

- Pages use `Component({...})` (glass-easel component framework, see `componentFramework` in `app.json`); page logic lives in `methods`, view data in `data`, updated via `this.setData()`. Tab pages use `pageLifetimes.show` for on-show work.
- Components set `options: { addGlobalClass: true }` so they can reuse the app-level flat classes (`.card`, `.btn`, `.sub`, …) and inherit the CSS-variable tokens.
- Paths between pages are relative (e.g. `wx.navigateTo({ url: '../today/today' })`); component references in `usingComponents` use absolute `/components/...`.
- Comments and UI text in the codebase are in Chinese; keep that style.
