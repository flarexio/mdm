# mdm-server

A minimal **Apple MDM** (Mobile Device Management) server written in Go, in the
[flarexio](https://github.com/flarexio) style: **Go-Kit + Clean Architecture + DDD/event sourcing**.

It focuses on the MDM management plane — device enrollment lifecycle, the two
device channels (check-in + command), and APNs wake-up. **Device identity / PKI
is delegated to external services**: `flarexio/identity` issues one-time SCEP
challenges and Step-CA signs the certificates. NanoMDM remains the reference for
a production-grade core; each module here mirrors its corresponding design.

## 邊界:管理平面 vs 身分平面

```
flarexio/identity + Step-CA          mdm-server (this repo)
  身分/PKI 平面                         Apple 管理平面
  - SCEP challenge generate/verify      - enrollment 生命週期
  - 憑證簽發 (Step-CA SCEP)              - check-in / command 兩通道
  協議無關                              - APNs 推播喚醒
                                        - .mobileconfig 組裝
```

裝置憑證的取得**不在本服務**:identity 發 challenge、Step-CA 簽憑證。mdm-server
只在組 enrollment profile 時向 identity 要一個 challenge 塞進 SCEP payload。

完整架構決策與實戰踩坑見 [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)。

## 架構

### 分層（Clean Architecture / Ports & Adapters）

transport 與 infra **同屬最外圈的 adapter**：transport 是 inbound（driving）、infra 是
outbound（driven）。兩側都**依賴內層 domain 的介面**，domain 不依賴任何 adapter。

```
+-----------------+     +---------------------+     +----------------------+
| transport/http  |     |  service.go (mdm)   |     | persistence/inmem    |
|  inbound        |     |                     |     |  outbound            |
| mTLS identity   |     |  domain:            |     | enrollment repo      |
| /checkin        | --> |  enrollment         | --> | command queue        |
| /server         |     |  command / checkin  |     | push (APNs)          |
+-----------------+     +---------------------+     +----------------------+
 driving adapter            domain core               driven adapter
```

- **組裝根** `cmd/mdm-server`：在最外層把每個 interface 綁到具體 adapter。
- **依賴方向一律向內**：transport ──▶ domain ◀── infra；domain **不 import** transport 或 infra。
- domain 定義 ports：`enrollment.Repository` · `command.Queue` · `push.Pusher`；
  outbound adapters 實作它們，service 透過介面使用，不知道背後是 inmem / APNs / Redis。

### 端到端流程（一條垂直切片）

```
① 註冊與取得身分憑證（PKI 在外部）
   POST /enroll（subject 取自 token）── 向 identity 取一次性 challenge ── 組 .mobileconfig 回傳
        │
        ▼
   裝置安裝 .mobileconfig（profile：SCEP payload + MDM payload，IdentityCertUUID 綁定）
        │  SCEP enroll（CSR + 一次性 challenge）
        ▼
   Step-CA SCEP provisioner ── challenge webhook 回呼 identity 驗 challenge ── 簽發
   裝置取得「身分憑證」= 之後連 MDM 的 mTLS client cert

② Check-in 通道（mTLS, PUT /checkin）
   裝置身分 = 驗過的 client cert CN（不信 body 的 UDID）
   Authenticate ─▶ Pending ── TokenUpdate(push 憑證) ─▶ Enrolled ── CheckOut ─▶ Removed

③ Command 通道（mTLS, PUT /server）— 非同步命令迴圈
   管理者 POST /enqueue/{subject} ─▶ 入佇列 ─▶ APNs 推播喚醒裝置（push/）
        │                                   （payload 只有 PushMagic）
        ▼
   裝置輪詢 Idle ─▶ 回一個命令 ─▶ 裝置回報 Acknowledged/Error/NotNow ─▶ 下一個 …… 空 200
        NotNow：保留、本連線跳過、下次重試
        推播回 410/Unregistered：裝置已不在 → 自動對帳成 Removed（補 CheckOut 的不可靠）
```

### 套件結構

```
cmd/mdm-server/    組裝根與可執行 server
service.go         根 package mdm：Service（check-in + command 兩通道）
enroll.go          Enroller：取 identity challenge → 組 enrollment .mobileconfig
command/           MDM 命令/結果協定型別 + Queue 介面（NotNow 語意）
checkin/           check-in 訊息 + 兩段式 discriminated-union 解碼
profile/           Configuration Profile（.mobileconfig）產生（SCEP + MDM + 信任錨 Certificate payload）
enrollment/        enrollment aggregate（狀態機）+ Repository 介面 + domain events
push/              APNs MDM 推播 client（憑證式）
identity/          對 flarexio/identity 的 client：取 SCEP challenge（走 mTLS）
auth/              admin endpoint 的 bearer token 驗證（identity JWKS, EdDSA）+ OPA 授權
conf/              config.yaml 載入
transport/http/    mTLS 身分中介層、/checkin、/server、/enroll、/enqueue、/enrollments
persistence/inmem/ 記憶體實作（command queue；測試用）
persistence/badger/ enrollment repo 的 BadgerDB 持久化（重啟存活）
```

## 怎麼跑

設定走 `<path>/config.yaml`（找不到會 fallback `config.example.yaml`）。`push` / `identity`
/ `enroll` 都在 config 裡，憑證路徑相對 `<path>`；CLI 只剩少數 operational flag。

1. `<path>/config.yaml`：照 `config.example.yaml` 填 `push` / `identity` / `enroll` / `auth`。
2. `<path>/permissions.json`：OPA 授權規則（repo 根目錄的 `permissions.json` 為預設，複製過去調整）。
3. `<path>/certs/`：放 `server.crt`、`server.key`、`ca.crt`（驗裝置憑證的 CA，通常是
   Step-CA root），以及 config 指到的 push、identity client 憑證。

```bash
go run ./cmd/mdm-server --mtls-enabled --path ./run
# 簽發： POST http://<host>:8080/enroll          → .mobileconfig（subject 取自 token）
# 裝置： PUT  https://<host>:8443/checkin、/server
#
# 下令： POST http://<host>:8080/enqueue/{subject}      → push 喚醒裝置（admin role）
# 查詢： GET  http://<host>:8080/enrollments[/{subject}] → 裝置與狀態（admin role）
```

admin endpoint 做兩段把關:
- **認證**:identity 簽的 bearer token(EdDSA,對 identity JWKS 驗)。token 的 `aud`
  要含 `auth.audience`,所以 identity 的 `jwt.audiences` 要加上 `mdm.flarex.io`。
- **授權**:OPA(`core/policy`)依 `<path>/permissions.json` 的 `role_permissions`
  比對每條路由的 `domain.action`,不符回 403。預設規則見 `permissions.json`。

角色分工:
- `POST /enroll`(`mdm::enroll.issue`)→ **`user` role**(自助註冊;subject 取自 token
  的 `sub`,使用者只能註冊自己)。identity 現在發的 `roles:["user"]` token 即可。
- `/enqueue`、`/enrollments`(`mdm::commands.*` / `mdm::enrollments.*`)→ **`admin` role**
  (identity 把 `jwt.admins` 名單內的使用者授予 `admin` role)。

設定 `auth` 區塊即啟用兩段(`/enroll` 需要 auth 才會掛上,因為 subject 來自 token)。

```bash
# 自助註冊：subject 取自 token 的 sub，回 .mobileconfig
curl -X POST http://<host>:8080/enroll -H "Authorization: Bearer <identity-jwt>"
```

operator API 需要 `admin` role。下令 body 是 `requestType` + 選填的 `command`(型別專屬欄位):

```bash
# 鎖屏（最直觀的 demo：發出去 iPhone 就鎖）
curl -X POST http://<host>:8080/enqueue/<subject> \
  -H "Authorization: Bearer <admin-jwt>" \
  -d '{"requestType":"DeviceLock"}'

# 查目前有哪些裝置、各在什麼狀態(pending/enrolled/removed、能不能 push)
curl -H "Authorization: Bearer <admin-jwt>" http://<host>:8080/enrollments
```

| Flag / Env | 說明 |
|---|---|
| `--path` / `MDM_PATH` | 工作目錄；config.yaml 與憑證讀自此（預設 `~/.flarex/mdm`） |
| `--port` / `MDM_HTTP_PORT` | 整合 server（health、enroll），預設 8080 |
| `--mtls-enabled` / `MDM_MTLS_ENABLED` | 啟用裝置端 mTLS server |
| `--mtls-port` / `MDM_MTLS_PORT` | 裝置端 mTLS port，預設 8443 |

config 設定項見 `config.example.yaml`：`push`（APNs 憑證）、`identity`（challenge 來源，mTLS）、
`enroll`（SCEP/MDM payload 靜態值）、`auth`（admin endpoint 的 JWKS 驗證）。

### 裝置通道 TLS（反向代理）

裝置通道（`/checkin`、`/server`）走 **mTLS**，**不能**穿過會終結 TLS 的反代。用 Traefik 時做
**TCP passthrough**（按 SNI 把原始 TLS 直送 mdm，mdm 自己終結 mTLS）：

```yaml
tcp:
  routers:
    mdm-device:
      entryPoints: [websecure]
      rule: "HostSNI(`device.mdm.flarex.io`)"   # 裝置用的 host；admin 仍走 mdm.flarex.io 一般 TLS
      tls:
        passthrough: true
      service: mdm-device
  services:
    mdm-device:
      loadBalancer:
        servers: [{ address: "mdm:8443" }]
```

裝置端點的 **server 憑證不必公開信任**：enroll 出的 `.mobileconfig` 會把 `certs/ca.crt`
（FlareX root）當成 **Certificate payload** 夾進去，裝置就信任 FlareX 簽的 MDM/SCEP server
憑證。對應 `enroll.externalURL` 指到裝置 host（如 `https://device.mdm.flarex.io`）。

### 測試

```bash
go test ./...
```

端到端（免真機，用 scepclient + curl 模擬裝置跑完整 SCEP 發證 + check-in + 命令迴圈）
見 [`docs/e2e-testing.md`](docs/e2e-testing.md)。

## 生產接法（重點）

- **身分/PKI**：用 **Step-CA** 當獨立 CA + **SCEP provisioner**，以 **challenge webhook**
  回呼 **`flarexio/identity`** 的 `/scep/challenge/verify` 做動態一次性 challenge 授權；
  mdm 組 profile 前向 identity `/scep/challenge/generate` 取 challenge。
  注意：SCEP 信封是 RSA，若 Step-CA intermediate 是 ECDSA，需另設 RSA `decrypter` 金鑰。
- **APNs**：MDM 推播是**憑證式**——需一張 **MDM Push Certificate**（vendor 簽 CSR →
  identity.apple.com）。一張憑證共用、推給所有裝置，裝置以各自的 token 區分。
- **TLS 終結**：裝置身分可來自 `r.TLS.PeerCertificates`（直連 / L4 passthrough）或反向代理
  轉發的 header。本服務預設讀 `r.TLS`，身分來源以 `IdentityFunc` 介面預留可替換。
- **儲存**：enrollment 用 **BadgerDB**（`persistence/badger`，資料在 `<path>/db`，重啟存活）；
  command queue 仍 in-memory（命令掉了重 enqueue 即可）。水平擴展時換共享 backend
  （Redis / DB），介面（`enrollment.Repository`、`command.Queue`）不變。

## License

MIT
