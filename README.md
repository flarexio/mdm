# mdm-server

A minimal Apple MDM (Mobile Device Management) server written in Go, built in the
[flarexio](https://github.com/flarexio) style: **Go-Kit + Clean Architecture + DDD/event sourcing**.

## 定位：學習載具，不是 NanoMDM 替代品

這個專案的目的是**徹底理解 Apple MDM 協定的每個原語**，用最小可行的程式碼親手實作一條
完整鏈路（裝置註冊 → 取得身分憑證 → 被 APNs 推播喚醒 → 拉取/回報命令）。

> 生產環境上，正確的做法是**整合 / 擴充 [NanoMDM](https://github.com/micromdm/nanomdm)**
> 而非重寫核心。本專案每個模組都會對照 NanoMDM 的對應設計，讓「手刻理解」與「整合能力」互相印證。

## 課程地圖

| 模組 | 內容 | NanoMDM 對照 |
|---|---|---|
| 0 | 專案骨架（本步驟） | repo 結構 |
| 1 | plist / MDM 命令領域模型 | `mdm/` |
| 2 | Configuration Profile + CMS 簽章 | profile 簽章 |
| 3 | SCEP 串接 StepCA（裝置身分憑證） | `scep` |
| 4 | MDM 協定核心：enroll / check-in + mTLS | `service/nanomdm`, `service/certauth` |
| 5 | 命令佇列（Redis / RabbitMQ 非同步派送） | `storage/` enqueue |
| 6 | APNs 推播喚醒 | `push/` |
| 7 | （選修）ABM / VPP 自動註冊 | `nanodep` |

## Architecture

Composition root 在 `cmd/mdm-server/main.go`，於此組裝
`repository → service → middleware → endpoint → transport`。

```
cmd/mdm-server/   程式進入點與組裝根
conf/             設定載入
<domain>/         領域 package（model、Repository interface、events）
service.go        Service interface + 實作 + middleware（根 package）
endpoint.go       Go-Kit EndpointSet
transport/        HTTP / pubsub adapters
persistence/      Repository 實作（inmem / kv / db）
```

## License

MIT
