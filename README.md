# mdm-server

A minimal Apple MDM (Mobile Device Management) server written in Go, built in the
[flarexio](https://github.com/flarexio) style: **Go-Kit + Clean Architecture + DDD/event sourcing**.

## 定位：學習載具，不是 NanoMDM 替代品

這個專案的目的是**徹底理解 Apple MDM 協定的每個原語**，用最小可行的程式碼親手實作一條
完整鏈路（裝置註冊 → 取得身分憑證 → 被 APNs 推播喚醒 → 拉取/回報命令）。

> 生產環境上，正確的做法是**整合 / 擴充 [NanoMDM](https://github.com/micromdm/nanomdm)**
> 而非重寫核心。本專案每個模組都對照 NanoMDM 的對應設計，讓「手刻理解」與「整合能力」互相印證。

## 課程地圖

| 模組 | 內容 | 狀態 | NanoMDM 對照 |
|---|---|---|---|
| 0 | 專案骨架 | ✅ | repo 結構 |
| 1 | plist / MDM 命令 + check-in 領域模型 | ✅ | `mdm/` |
| 2 | Configuration Profile + CMS 簽章 | 簽章待補 | profile 簽章 |
| 3 | SCEP 串接 StepCA（裝置身分憑證） | ✅ | `scep` |
| 4 | MDM 協定核心：enroll / check-in + mTLS | ✅ | `service/nanomdm`, `service/certauth` |
| 5 | 命令佇列（NotNow 語意、可換持久 backend） | ✅ | `storage/` enqueue |
| 6 | APNs 推播喚醒（憑證式）+ 閉環對帳 | ✅ | `push/` |
| 7 | （選修）ABM / VPP / declarative management | — | `nanodep` |

## 架構

### 分層（Clean Architecture / Ports & Adapters）

transport 與 infra **同屬最外圈的 adapter**：transport 是 inbound（driving）、infra 是
outbound（driven）。兩側都**依賴內層 domain 的介面**，domain 不依賴任何 adapter。

```
+-----------------+     +---------------------+     +----------------------+
| transport/http  |     |  service.go (mdm)   |     | persistence/inmem    |
|  inbound        |     |                     |     |  outbound            |
| mTLS identity   |     |  domain:            |     | repo/queue/challenge |
| /checkin        | --> |  enrollment         | --> | ca / scep            |
| /server         |     |  command / checkin  |     | push (APNs)          |
| scep webhook    |     |  profile / challenge|     |                      |
+-----------------+     +---------------------+     +----------------------+
 driving adapter            domain core               driven adapter
```

- **組裝根** `cmd/mdm-server`：在最外層把每個 interface 綁到具體 adapter。
- **依賴方向一律向內**：transport ──▶ domain ◀── infra；domain **不 import** transport 或 infra。
- domain 定義 ports：`enrollment.Repository` · `command.Queue` · `push.Pusher` · `challenge.Store`；
  outbound adapters 實作它們，service 透過介面使用，不知道背後是 inmem / StepCA / APNs / Redis。

### 端到端流程（一條垂直切片）

```
① 註冊與取得身分憑證
   裝置安裝 .mobileconfig（profile：SCEP payload + MDM payload，IdentityCertUUID 綁定）
        │  SCEP enroll（CSR + 一次性 challenge）
        ▼
   SCEP server（scep/）── 驗 challenge（challenge/）+ CN 綁定 ── 簽發（ca/ 或 StepCA）
        └─ 生產：StepCA SCEP provisioner + webhook → POST /scep/challenge/verify
   裝置取得「身分憑證」= 之後連 MDM 的 mTLS client cert

② Check-in 通道（mTLS, PUT /checkin）
   裝置身分 = 驗過的 client cert CN（不信 body 的 UDID）
   Authenticate ─▶ Pending ── TokenUpdate(push 憑證) ─▶ Enrolled ── CheckOut ─▶ Removed

③ Command 通道（mTLS, PUT /server）— 非同步命令迴圈
   管理者 Enqueue(cmd) ─▶ 入佇列 ─▶ APNs 推播喚醒裝置（push/）
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
command/           MDM 命令/結果協定型別 + Queue 介面（NotNow 語意）
checkin/           check-in 訊息 + 兩段式 discriminated-union 解碼
profile/           Configuration Profile（.mobileconfig）產生（SCEP + MDM payload）
enrollment/        enrollment aggregate（狀態機）+ Repository 介面 + domain events
ca/                最小 X.509 CA（開發/測試簽發後端；生產用 StepCA）
challenge/         一次性 SCEP challenge Store 介面
scep/              SCEP server 接線（micromdm/scep + 自有 CA + challenge）
push/              APNs MDM 推播 client（憑證式）
transport/http/    mTLS 身分中介層、/checkin、/server、SCEP challenge webhook
persistence/inmem/ 記憶體實作（enrollment repo、challenge store、command queue）
```

## 怎麼跑

### 最小 demo（只起整合/管理 server，無需憑證）

```bash
go run ./cmd/mdm-server --scep-webhook-secret dev-secret
curl localhost:8080/healthz          # → 200
```

啟用 `--scep-webhook-secret` 後，StepCA 可呼叫 `POST /scep/challenge/verify`
（以 `X-Smallstep-Signature` HMAC 認證）。未設 push 憑證時，推播改為 logging no-op。

### 完整（裝置端 mTLS + 真實 APNs）

在 `<path>/certs/` 放三個檔：`server.crt`、`server.key`（server TLS）、`ca.crt`
（驗裝置憑證的 CA，通常是你 StepCA 的 root）。

```bash
go run ./cmd/mdm-server \
  --mtls-enabled --path ./run \
  --scep-webhook-secret "$SECRET" \
  --push-cert push.pem --push-key push-key.pem
# 裝置端：PUT https://<host>:8443/checkin、PUT https://<host>:8443/server
```

| Flag / Env | 說明 |
|---|---|
| `--path` / `MDM_PATH` | 工作目錄；mTLS 憑證讀自 `<path>/certs/` |
| `--port` / `MDM_HTTP_PORT` | 整合 server（webhook、health），預設 8080 |
| `--mtls-enabled` / `MDM_MTLS_ENABLED` | 啟用裝置端 mTLS server |
| `--mtls-port` / `MDM_MTLS_PORT` | 裝置端 mTLS port，預設 8443 |
| `--scep-webhook-secret` / `MDM_SCEP_WEBHOOK_SECRET` | StepCA SCEP challenge webhook 的 HMAC 密鑰 |
| `--push-cert` / `--push-key` | MDM Push 憑證（PEM）；設定後啟用真實 APNs |

### 測試

```bash
go test ./...
```

## 生產接法（重點）

- **CA**：用 **StepCA** 當獨立服務（`step-ca`），加一個 **SCEP provisioner**，
  以 **challenge webhook** 回呼本服務的 `/scep/challenge/verify` 做動態一次性 challenge 授權。
  `ca/`、`scep/`（內建簽發）僅供開發/自學；`challenge/`、`profile/` 可直接進生產。
  注意：SCEP 信封是 RSA，若 StepCA intermediate 是 ECDSA，需另設 RSA `decrypter` 金鑰。
- **APNs**：MDM 推播是**憑證式**——需一張 **MDM Push Certificate**（vendor 簽 CSR →
  identity.apple.com）。一張憑證共用、推給所有裝置，裝置以各自的 token 區分。
- **TLS 終結**：裝置身分可來自 `r.TLS.PeerCertificates`（直連 / L4 passthrough）或反向代理
  轉發的 header。本服務預設讀 `r.TLS`，身分來源以 `IdentityFunc` 介面預留可替換。
- **儲存**：`persistence/inmem` 適合單實例/開發；水平擴展時換共享 backend（Redis / DB），
  介面不變。

## License

MIT
