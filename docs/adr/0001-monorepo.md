# 单仓 monorepo

前端（微信小程序 TS）与后端（Go）共存于同一仓库：`miniprogram/` + `backend/` + `docs/`；issue 跟踪用本地 markdown（`.scratch/`）。前后端 API 契约需要高频同步演进，单人开发 + AI 协作的场景下分仓只会增加摩擦；未来扩展 iOS App 时在仓内加 `app/` 即可。
