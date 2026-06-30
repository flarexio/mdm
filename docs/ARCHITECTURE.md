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

## 5.1 一致性模型(分散式準備)

為了未來水平擴展,enrollment 寫入走 **事件溯源 + 強一致快取橋接** 的混合模型:

```
寫: 動作 → 強一致寫 Redis Cache → Notify() 發事件 ─┐
讀(durable): EventHandler 訂閱事件 → Store 進 durable repo(最終一致)
```

- **為何需要 Cache**:`Authenticate` 與**第一次** `TokenUpdate` 是裝置秒級、自動、可能落在
  不同 instance 的連續握手;而 durable 由事件 handler 非同步寫入,有 lag。`Authenticate`
  **同步寫 Redis(共享、強一致)**,讓緊接著的第一次 `TokenUpdate` 跨 instance 也讀得到。
- **Cache 只給 check-in 握手用**:讀取邊界收得很窄——

  | 方法 | 讀取來源 |
  |---|---|
  | `TokenUpdate` | durable → **Cache** → upsert(唯一有緊耦合 read-after-write 的訊息) |
  | `Authenticate` | 只寫(建立) |
  | `CheckOut` / `wake`(command) | **只讀 durable**(teardown / 已 enrolled 裝置,不在握手窗口) |

- **upsert**:`TokenUpdate` 在 durable 與 Cache 都 miss 時,直接用訊息本身建 Enrolled。Apple
  明文「**不保證重發 TokenUpdate**」,且 mTLS 憑證已驗過身分 → 寧可 upsert 也不 reject。
- **Cache TTL**:只需撐過事件處理 lag(預設 1h);過期無害,因為 upsert 兜底。
- **事件帶完整快照**:不同事件型別走不同 subject(`enrollments.<id>.<name>`),跨 subject 無
  順序保證 → 事件自帶整個 enrollment 快照,handler 就是冪等 `Store`,容忍重送與亂序。
- **bus = NATS JetStream**:正式以 NATS JetStream 跑(`cmd/mdm-server` 接 `AddJetStream` +
  `AddStreamAndConsumer` + `PullConsume`,`events.ReplaceGlobals` 讓 `Notify` 發到 NATS)。
  **consumer 是 per-instance**:durable 是各 instance 本機 badger,所以每個 instance 都要收到全部
  事件、各自建副本,不能用共享 queue group。consumer durable name 來自 config `name`(yaml 以
  `$INSTANCE_NAME` 展開);K8s StatefulSet 把 `INSTANCE_NAME` 設成 pod name(穩定 0-based ordinal
  `mdm-0`/`mdm-1`…)。測試用 `inmem` 同步 pubsub(`Notify` 即 read-your-write),介面相同。

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

- **enrollment(durable)** → **BadgerDB**(`persistence/badger`,`<path>/db`,重啟存活),由
  EventHandler 寫入(最終一致)。
- **enrollment(cache)** → **Redis**(`persistence/redis`,正式用);測試用 in-memory
  (`persistence/inmem`)。只放握手橋接,短 TTL,見 §5.1。
- **command queue** → **Redis**(`persistence/redis`,正式用);測試用 in-memory。每個 enrollment
  的 queue 存成單一 JSON value,用樂觀鎖(WATCH/MULTI)做原子 read-modify-write。沿用「peek 隊頭、
  terminal 才移除」語意,所以可靠重投內建,不需 processing list / visibility timeout。

## 10. 部署拓樸

```
iPhone ──443/SNI── Traefik ──┬─ mdm.flarex.io(終結 TLS)──▶ mdm admin :8080
                              └─ device.mdm.flarex.io(passthrough)──▶ mdm mTLS :8443
mdm ──mTLS──▶ identity :8443(取 challenge)   mdm ──▶ APNs(push)
mdm ──▶ Badger(<path>/db,durable)   mdm ──▶ Redis(cache)   mdm ──▶ NATS JetStream(event bus)
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
  SCEP challenge TTL 調長(預設 5 分鐘,真機 UX 偏緊)。
- **分散式收尾**:事件溯源 + NATS JetStream + Redis cache + Redis command queue 已就位。
- **command 抽象 + 結果事件**(已完成):commands 收斂成 typed `Request` + registry(只有實作的能下);
  結果 plist 解成 typed domain model(`DecodeResponse`,用命令的 `RequestType` 挑 decoder);terminal
  結果發 `command_responded` 事件(在命令還在 queue 時發、發成功才推進,orphan 也照發),用
  `CommandUUID` 對應請求方。**待補**:prod 要消費這個事件需加 `commands.>` 的 JetStream stream +
  consumer(目前只發,沒人消費前不持久化)。
- **TokenUpdate merge 語意**:目前直接覆蓋 `Push`;待落實 Apple「UnlockToken 沒帶別清、
  PushMagic 變才更新」(需在 aggregate 加 UnlockToken 欄位)。
