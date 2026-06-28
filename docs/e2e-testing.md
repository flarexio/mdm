# 端到端測試（免真機）

用 `scepclient` + `curl` 模擬一台裝置,把整個管理平面跑一遍 —— enroll →
**真 SCEP 發證** → mTLS check-in → 命令迴圈。不需要 iPhone、不需要 APNs push 憑證
（裝置改用主動輪詢取代被喚醒）。

驗證範圍:admin token（JWKS）認證 + OPA 授權、`/enroll` 組 profile（含向 identity
取一次性 challenge）、Step-CA SCEP 動態 challenge 發證、裝置 mTLS 通道、check-in
狀態機、command enqueue/輪詢/回報。

## 前置

- `scepclient`（`go install github.com/micromdm/scep/v2/cmd/scepclient@latest`）
- 一個 identity 簽的 token（`aud` 含 `mdm.flarex.io`;`roles` 要有 `admin` 才能用
  operator 端點 —— 由 identity 的 `jwt.admins` 授予）
- 系統信任有 **FlareX root**(curl 才驗得過 `device.mdm.flarex.io` 的 server 憑證),
  或各 curl 帶 `--cacert ca.crt`
- 可連到 `mdm.flarex.io`（admin, HTTP）、`ca.flarex.io`（SCEP）、`device.mdm.flarex.io`
  （裝置 mTLS, Traefik passthrough → mdm:8443）

```bash
TOKEN='<identity-jwt>'
```

## ① 取 enrollment profile，抽出 challenge

```bash
curl -sS -X POST https://mdm.flarex.io/enroll -H "Authorization: Bearer $TOKEN" \
  -o enroll.mobileconfig -w "HTTP %{http_code}\n"

CH=$(python3 -c "import plistlib;p=plistlib.load(open('enroll.mobileconfig','rb'));\
print(next(x for x in p['PayloadContent'] if x['PayloadType']=='com.apple.security.scep')['PayloadContent']['Challenge'])")
echo "challenge=$CH"   # Subject CN = token 的 sub
```

## ② SCEP 發裝置憑證

```bash
D=$(mktemp -d); cd "$D"        # 全新空目錄 + 全新 key（殘留檔會送出殘缺 body）
scepclient \
  -server-url https://ca.flarex.io/scep/mdm-scep \
  -challenge "$CH" \
  -cn mirror770109 \
  -organization FlareX \
  -key-encipherment-selector \
  -private-key device.key \
  -certificate device.crt

openssl x509 -in device.crt -noout -subject -ext extendedKeyUsage
#   Subject: O=FlareX, OU=MDM, CN=mirror770109   （OU 由 mdm-scep 模板強制）
#   EKU:     TLS Web Client Authentication
```

challenge 是一次性的:若 scepclient 失敗、challenge 已被消耗,要回 ① 重拿 profile。

## ③ Check-in（用裝置憑證）

```bash
DEV=(--cert device.crt --key device.key)   # 系統信任有 FlareX root；否則加 --cacert ca.crt

# Authenticate -> Pending
curl -sS "${DEV[@]}" -X PUT https://device.mdm.flarex.io/checkin -w "\nHTTP %{http_code}\n" --data-binary @- <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
  <key>MessageType</key><string>Authenticate</string>
  <key>UDID</key><string>REAL-0001</string>
  <key>Topic</key><string>com.apple.mgmt.External.test</string>
</dict></plist>
EOF

# TokenUpdate -> Enrolled
curl -sS "${DEV[@]}" -X PUT https://device.mdm.flarex.io/checkin -w "\nHTTP %{http_code}\n" --data-binary @- <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
  <key>MessageType</key><string>TokenUpdate</string>
  <key>UDID</key><string>REAL-0001</string>
  <key>Topic</key><string>com.apple.mgmt.External.test</string>
  <key>PushMagic</key><string>MAGIC-REAL</string>
  <key>Token</key><data>AQIDBA==</data>
</dict></plist>
EOF
```

## ④ admin 確認裝置

```bash
curl -sS -H "Authorization: Bearer $TOKEN" https://mdm.flarex.io/enrollments
# [{"id":"mirror770109","status":"enrolled","can_push":true,...}]   id = 裝置憑證的 CN
```

## ⑤ 命令迴圈（enqueue → 輪詢 → 回報）

```bash
# 下命令（admin；subject = 裝置 CN）
curl -sS -H "Authorization: Bearer $TOKEN" -X POST https://mdm.flarex.io/enqueue/mirror770109 \
  -d '{"requestType":"DeviceInformation","command":{"Queries":["DeviceName"]}}'

# 裝置輪詢 Idle -> 拿到命令
curl -sS "${DEV[@]}" -X PUT https://device.mdm.flarex.io/server --data-binary @- <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>Status</key><string>Idle</string></dict></plist>
EOF

# 回報 Acknowledged（CommandUUID 用上一步回來的）-> 同一回應吐下一個命令，佇列空時回空 200
curl -sS "${DEV[@]}" -X PUT https://device.mdm.flarex.io/server --data-binary @- <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
  <key>Status</key><string>Acknowledged</string>
  <key>CommandUUID</key><string><上一步的 CommandUUID></string>
  <key>QueryResponses</key><dict><key>DeviceName</key><string>TestDevice</string></dict>
</dict></plist>
EOF
```

## 坑

- **SCEP URL** 必須是 `<ca>/scep/<provisioner>` = `https://ca.flarex.io/scep/mdm-scep`。
  少了 provisioner 名(如 `/scep/mdm`)→ Step-CA 回 `provisioner mdm not found`。
  這也是 `enroll.scep.url` 要填的值。
- **`-key-encipherment-selector`**:CA 同時有 ECDSA intermediate 和 RSA decrypter 時,
  scepclient 要靠它挑出有 keyEncipherment 的 RSA 那張,否則 `pkcs7: only RSA keys are
  supported`。真 iOS 會自己挑,這是 client 端的事。
- **裝置 server 憑證**:`device.mdm.flarex.io` 走 Traefik **TCP passthrough**(不終結 TLS),
  mdm 自己終結 mTLS;server 憑證放 mdm `certs/server.*`(FlareX 簽、SAN 含 host、fullchain)。
  裝置不需公開憑證 —— profile 夾了 FlareX root 當信任錨。

## 這套沒涵蓋的（要真 iPhone）

- **APNs push**:profile 的 `Topic`(來自 push 憑證)、以及 server 主動喚醒睡著的裝置。
  此測試靠裝置主動輪詢,不需要。
- iOS 內建的 SCEP/MDM client、真機裝 profile（Safari/email/QR → 設定）。
