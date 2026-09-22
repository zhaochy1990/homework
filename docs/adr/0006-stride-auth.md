# 认证复用 STRIDE auth-service

不自建用户体系。登录走现有自研 Go 认证服务（OAuth2 grant 体系，微信小程序免密登录 grant + 手机号验证码 grant），后端作业服务对签发的 JWT 做本地验签（服务密钥运行时挂载，部署同在腾讯云）。手机号登录能力保留在认证服务中，但 v1 小程序界面不出现。spec 见 stride-devops#335，ADR 见 stride/auth 仓库 docs/adr/0001–0011。
