# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

WeChat Mini Program written in TypeScript, based on the official `miniprogram-ts-quickstart` template. There is no CLI build/lint/test tooling — compilation, preview, and upload are done through **WeChat DevTools (微信开发者工具)**, which reads `miniprogram/project.config.json`. The DevTools TypeScript compiler plugin compiles `.ts` files automatically; do not write compiled `.js` files by hand.

## Structure

- `project.config.json` (repo root of this dir) — DevTools config; `miniprogramRoot`/`srcMiniprogramRoot` point to the inner `miniprogram/` directory. `appid` is configured here.
- `miniprogram/` — actual source root:
  - `app.ts` / `app.json` / `app.wxss` — app entry. New pages must be registered in `app.json` `pages` array.
  - `pages/index/`, `pages/logs/` — pages; each page is a 4-file set: `.ts` (logic), `.wxml` (template), `.wxss` (styles), `.json` (page config).
  - `utils/` — shared helpers (e.g. `formatTime`).
  - `typings/` — ambient types, including `IAppOption` (global `App`/`getApp` option type) defined in `typings/index.d.ts`.
- `tsconfig.json` — strict mode is fully enabled (`strict`, `noUnusedLocals`, `noUnusedParameters`, etc.), so unused variables/params will fail compilation.

## Conventions

- The index page uses `Component({...})` (glass-easel component framework, see `componentFramework` in `app.json`) rather than `Page({...})` — page logic lives in `methods`, view data in `data`, updated via `this.setData()`. New pages may follow either pattern; match the surrounding page.
- Paths between pages are relative (e.g. `wx.navigateTo({ url: '../logs/logs' })`).
- Comments and UI text in the codebase are in Chinese; keep that style.
