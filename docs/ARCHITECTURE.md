# 架構與設計筆記

一套從零自建、真機驗證過的 **Apple MDM** server:從 `/enroll` 一路到「遠端把真
iPhone 鎖起來」。本文記架構決策與實戰踩過的坑。

## 1. 系統定位

Apple MDM 是 **OS 層、pull-based** 的協議,**不需要 app**。裝置裝一份 enrollment
`.mobileconfig`,之後由 OS 內建的 MDM framework 跟 server 互動。本服務只做 **Apple
管理平面**;**裝置身分 / PKI 委派給外部**(`flarexio/identity` + Step-CA)。

## 2. 邊界:兩個 bounded context

| | `flarexio/identity` + Step-CA | `mdm`(本 repo) |
|---|---|---|
| 平面 | 身分 / PKI | Apple 管理 |
| 職責 | SCEP challenge 發/驗、憑證簽發 | enrollment 生命週期、兩通道、APNs、profile |
| 協議 | 協議無關 | Apple MDM 專屬 |

mdm 只在組 profile 時向 identity 要一次性 challenge,塞進 SCEP payload。

## 3. 分層(Clean Architecture / Ports & Adapters)

```
transport/http (inbound) ──▶ service.go / domain ◀── persistence, push (outbound)
```
- domain 定義 ports:`enrollment.Repository`、`command.Queue`、`push.Pusher`、`identity.Challenger`。
- 組裝根 `cmd/mdm-server` 在最外層把介面綁具體 adapter;依賴一律向內。
- 換 in-memory→BadgerDB、加 APNs、換反代 header 取憑證,domain 都不動。

## 4. Enrollment 端到端

```
POST /enroll (user token)
  → 向 identity 取一次性 challenge → 組 .mobileconfig(SCEP + MDM + 信任錨 payload）
裝置裝 profile
  → SCEP:device 自產 keypair + CSR + challenge → Step-CA
     → Step-CA SCEPCHALLENGE webhook 回呼 identity Verify(HMAC + 一次性 consume）
     → 簽出裝置身分憑證(CN=subject, OU=MDM, clientAuth)
  → MDM check-in(mTLS,用上面那張憑證當 client cert)
```
- **subject 取自 token 的 `sub`**,不從 URL → 使用者只能註冊自己。
- challenge 是動態一次性祕密;靜態 challenge = 死值,所以走 webhook。

## 5. 兩個裝置通道(device-facing, mTLS)

- **Check-in(`PUT /checkin`)**:Authenticate → Pending → TokenUpdate(存 APNs 憑證)
  → Enrolled → CheckOut → Removed。裝置身分 = **驗過的 client cert CN**,不信 body 的 UDID。
- **Command(`PUT /server`)**:非同步迴圈。一個請求**同時回報前一個結果 + 拿下一個命令**;
  佇列 FIFO,空 200 = 沒事了。`NotNow` 保留重試;APNs 回 410/Unregistered → 自動對帳成 Removed。

## 6. 認證與授權(admin endpoints)

兩段:
- **authn**:identity 簽的 EdDSA bearer token,對 identity **JWKS**(public endpoint,系統信任)驗。
- **authz**:**OPA**(`core/policy`)依 `permissions.json` 比對每條路由的 `domain.action`。
- 角色:`/enroll`=**user**(自助);`/enqueue`、`/enrollments`=**admin**。
  admin role 由 identity 的 `jwt.admins` allowlist 授予。

> 坑:identity 簽 token **不帶 `kid`** → verifier 在「JWKS 單一把 key」時 fallback。

## 7. PKI:SCEP 動態 challenge

```
identity.Generate(subject) → challenge ──┐(塞進 profile)
                                          ▼
device → Step-CA PKCSReq(challenge) → SCEPCHALLENGE webhook → identity.Verify
                                          ▼ allow
                                   Step-CA 簽發裝置憑證
```
- Step-CA intermediate 是 **ECDSA** → SCEP 的 RSA 信封解不開 → 另立一張 **RSA decrypter** 葉憑證。
- provisioner 模板**強制 `OU=MDM`、砍 serverAuth、CN 取自 CSR** → 裝置改不了 subject。
- 跟 OpenVPN(`OU=VPN`)做**憑證用途隔離**:OpenVPN `tls-verify` 只收 `OU=VPN`,
  + 模板鎖死 `OU=MDM` → MDM 憑證拿不到也偽造不出 VPN 權限。

## 8. TLS / 信任設計

裝置會碰三個 TLS 端點,**都不需要公開憑證**:
- 信任靠 **profile 夾的 FlareX root(Certificate payload)** 當錨 → 裝置信任 FlareX 簽的
  `ca.flarex.io`(SCEP)、`device.mdm.flarex.io`(MDM)。
- **mTLS 不能穿 TLS-terminating 反代**(反代解密 → client 憑證到不了)。選 **Traefik TCP
  passthrough**(按 SNI 直送 mdm:8443,mdm 自己終結 mTLS),不改裝置 code。
  - `mdm.flarex.io` → 終結 TLS → admin:8080;`device.mdm.flarex.io` → passthrough → mdm:8443。
    同 443 靠 SNI 分流共存。
- 另一條路是 **Mode B**(反代終結 mTLS + `passTLSClientCert` header,mdm 從 header 讀憑證)—
  一個 host 全包但要改 code + 防 header 偽造。本專案選 Mode A。

## 9. 持久化

- **enrollment** → **BadgerDB**(`persistence/badger`,`<path>/db`,重啟存活)。
- command queue → in-memory(掉了重 enqueue)。介面不變,水平擴展換共享 backend 即可。

## 10. 部署拓樸

```
iPhone ──443/SNI── Traefik ──┬─ mdm.flarex.io(終結 TLS)──▶ mdm admin :8080
                              └─ device.mdm.flarex.io(passthrough)──▶ mdm mTLS :8443
mdm ──mTLS──▶ identity :8443(取 challenge)   mdm ──▶ APNs(push)   mdm ──▶ Badger(<path>/db)
```
image `flarexio/mdm`,GitHub Actions build/release,config 走 `<path>/config.yaml`(預設 `~/.flarex/mdm`)。

## 11. 實戰踩過的坑(debugging notes)

- **SCEP URL**:Step-CA 端點是 `<ca>/scep/<provisioner>`;少了 `-scep`(`/scep/mdm`)→
  `provisioner not found`。真機才看得到(替身憑證跳過了 SCEP)。
- **scepclient `-key-encipherment-selector`**:CA 同時有 ECDSA intermediate + RSA decrypter,
  要挑出有 keyEncipherment 的那張,否則 `pkcs7: only RSA keys are supported`。真 iOS 會自己挑。
- **iOS 398 天 TLS 憑證效期上限**:device server 憑證用預設簽 **3 年** → iPhone 握手 **EOF**
  (curl 不在乎所以本機測 200,iOS 硬拒)。重簽 `--not-after 8760h`(365 天)解。Apple 對
  **所有** TLS server 憑證(含私有 CA)都套這上限。
- **信任錨時機**:profile 簽章在「裝 profile 前」就驗,那時 FlareX 錨還沒裝 → 要綠勾「已驗證」
  得用**公開憑證**簽,FlareX 自簽只能「未驗證」。
- **APNs 是憑證式**:走 mdmcert.download(社群 vendor 代簽)→ identity.apple.com 發證;
  Topic = 憑證 Subject 的 `UID`(`com.apple.mgmt.External.<UUID>`),mdm 自動讀出。

## 12. 未竟事項(皆 optional)

- profile CMS 簽章(綠勾)、identity Verify 綁定 challenge↔subject(防 CN 冒充)、
  command queue 持久化、SCEP challenge TTL 調長(預設 5 分鐘,真機 UX 偏緊)、
  完整 flarexio 事件溯源(NATS event bus)。
