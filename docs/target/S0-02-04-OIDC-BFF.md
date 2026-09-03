# S0-02-04 — Web BFF OIDC 登录

## 范围

`web` 提供两个服务端路由：

- `GET /auth/login`：创建一次性登录事务并以 `302` 跳转到已发现的 OIDC 授权端点。
- `GET /auth/callback`：消费事务、在服务端完成 Authorization Code + PKCE 交换并验证 ID Token；成功时只返回 `{"status":"verified"}`。

这是厂商中立的 OpenID Connect Authorization Code Flow。使用 `openid-client` 完成发现、授权请求和授权码交换，使用 `jose` 从已发现的 JWKS 验证 ID Token 的 RS256 签名、精确 issuer、audience 和过期时间；nonce 由 BFF 与事务中保存的随机值精确比较。

## 配置

所有变量仅由服务端读取，名称均不带 `NEXT_PUBLIC_`：

| 变量 | 用途 |
| --- | --- |
| `OIDC_ISSUER` | OIDC issuer 标识符；生产必须是 HTTPS，且不能带 query 或 fragment。 |
| `OIDC_CLIENT_ID` | 已注册客户端标识。 |
| `OIDC_CLIENT_SECRET` | 用于 token endpoint 的 client-secret basic 身份验证；绝不出现在 URL、响应或日志。 |
| `OIDC_REDIRECT_URI` | 已注册的固定回调地址；必须精确为 `/auth/callback`，不得带 query 或 fragment。 |
| `OIDC_ALLOW_INSECURE_TEST_HTTP=1` | 仅为本机测试显式开放 `http://127.0.0.1` 或 `http://localhost` 的 issuer 和回调地址。未设置即拒绝 HTTP。 |

缺失、空白或不安全的配置会失败关闭。调用方不能提供回跳地址，因此没有开放重定向入口。

## 状态机与数据边界

```text
/auth/login
  -> 发现 provider
  -> 生成 state / nonce / PKCE verifier 和 S256 challenge
  -> 内存事务表[state] = { nonce, verifier, issuer, clientId, redirectUri, expiresAt }
  -> 302 authorize (仅 code、openid email、state、nonce、challenge)
  -> /auth/callback
       -> 先原子消费 state
       -> provider error 映射 或 code+PKCE 交换
       -> JWKS 签名 + issuer + audience + expiry + nonce 验证
       -> VerifiedLogin 完成接口（当前无会话实现）
       -> no-store {"status":"verified"}
```

内存事务存储有 10 分钟 TTL、单次消费和 1000 条容量上限；实现通过接口与路由分离，并以同一 Node 进程的服务端全局单例跨 Next 路由 bundle 共享，当前仅适用于本地单实例。事务不保存 provider token。失败、过期、未知或已消费的 state 都返回稳定的 `invalid_state`。事务容量耗尽返回安全的 `login_unavailable`，不会降级为不带 state 的登录。

`VerifiedLogin` 仅包含规范化的 `{ issuer, subject, email?, displayName? }`，并通过 `VerifiedLoginCompleter` 作为 S0-02-05 服务端会话接续点。S0-02-04 不设置认证 Cookie、不写 local/session storage、不实现 CSRF/注销、不放行主页面，也不接入 Principal、Go 身份绑定、权限或审计。

## provider error 与失败响应

所有回调响应均为 `Cache-Control: no-store`，且绝不反射 `error_description`、`error_uri`、token endpoint 正文或 provider 原始错误。

| 条件 | 状态 | 稳定响应代码 |
| --- | ---: | --- |
| 缺失、未知、过期或重放 state | 400 | `invalid_state` |
| `access_denied` | 400 | `provider_access_denied` |
| `temporarily_unavailable` | 400 | `provider_temporarily_unavailable` |
| `server_error` | 400 | `provider_server_error` |
| 其他 provider `error` | 400 | `provider_error` |
| state 有效但缺少 code | 400 | `missing_code` |
| token endpoint、签名、nonce、issuer、audience 或 expiry 验证失败 | 401 | `authentication_failed` |
| 配置在 callback 时改变或不安全 | 500 | `configuration_error` |

## 验证与未覆盖项

`web/tests/oidc-bff.test.ts` 使用仅本机的 mock discovery、authorize、token 和 JWKS 服务，覆盖安全的 302 参数、成功回调、provider access denied、未知/重放 state、缺 code、TTL/一次性/容量/随机值、nonce/issuer/audience/expiry/签名错误和 token endpoint 失败。测试不记录 token、client secret 或 verifier 原文。

浏览器验收（本机 2026-09-03）：应用内 Browser 打开 `http://127.0.0.1:5187/auth/login` 后到达本机 mock authorize 页面，标题为 `Mock OIDC authorization`，DOM 非空、无 console warn/error，批准链接唯一且已交互一次。Next 本机日志记录该次 `/auth/login` 的 302 和随后 `/auth/callback` 的 200。可视截图在仓库外 `C:\Users\hul\AppData\Local\Temp\s0-02-04-oidc-browser-authorize.png`，画面仅含本机 mock 授权标题与批准链接。Browser 随后会阻止将携带授权码的 callback URL 直接保留在另一个标签页（`ERR_BLOCKED_BY_CLIENT`）；因此 verified JSON 的浏览器可视 title/截图未覆盖，成功响应由同一浏览器交互触发的 Next 200 日志及自动化集成测试共同证明。

本任务未覆盖真实 IdP 的注册配置、分布式事务存储、会话/Cookie、CSRF、注销、Principal、用户身份绑定、权限、审计或生产部署。
