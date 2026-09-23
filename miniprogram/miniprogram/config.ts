// 运行环境配置。真机登录走下面的公网地址；DevTools 本地联调时可在「详情 → 本地设置」
// 勾选「不校验合法域名」并把地址临时改成本机后端。
export const AUTH_BASE_URL = 'https://api.stride-running.cn'
export const API_BASE_URL = 'https://homework.stride-running.cn/api/v1'

// STRIDE 公开客户端 id（小程序 application）：token_exchange 的 client_id，
// 同时是作业后端 JWT aud 白名单值（docs/research/001）。
export const CLIENT_ID = 'app_573f41b46a644fb9ae6b6250'
