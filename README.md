# 半雲電話 · HalfCloud

自託管 Android 畫面遙控：WebRTC 聲畫、觸控、文字輸入、多控制端、同一網址切換受控電話，以及首次電話批准後自動登入嘅信任控制端。

## 下載

目前版本為 **0.4.1 / 1041**，提供 iOS 控制端、Android 控制端、Android 受控端及 VPS 服務。控制端使用原生介面，iOS 支援互動 Liquid Glass。

APK、iOS IPA 及 VPS 安裝包見 [0.4.1 發佈頁](https://github.com/sing0626/remote-console/releases/tag/v0.4.1)。iOS IPA 需自行簽署後安裝。

0.4.1 改用以下 App ID，會建立獨立 App。升級時請先保留舊 App，新控制端重新批准一次即可。

| 元件 | App ID / Bundle ID |
| --- | --- |
| Android 控制端 | `com.sing.halfcloud.controller` |
| Android 受控端 | `com.sing.halfcloud.server` |
| iOS 控制端 | `com.sing.halfcloud.controller` |

受控端的 Root 工作目錄為 `/data/local/tmp/remote-console`；既有部署沿用原本位置。

## 安裝與連接

全新 Ubuntu / Debian 主機以 root 執行：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/sing0626/remote-console/main/install.sh)
```

安裝器會準備 Python、OpenSSL、Docker Compose，並詢問域名、公網 IPv4 及電話公鑰。完成後按輸出設定 HTTPS 反代及防火牆，輸入 `remote-console` 即可開啟管理選單。

在控制端輸入部署網址，選擇受控電話；於電話上核對名稱及代碼後批准，即可開始遙控。

## 升級

既有伺服器以 root 執行：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/sing0626/remote-console/v0.4.1/install.sh) upgrade --release v0.4.1
remote-console
```

安裝器會選擇一般更新或 v0.1.14 遷移，先備份再切換服務，保留電話配對及控制端授權。切換時服務會短暫重啟。

## 原始碼

- `app/`：需要 Root 嘅 Android 受控端。
- `controller/`：原生 Android 控制端；使用者自行輸入網址。
- `clients/ios/`：原生 SwiftUI／WebRTC 控制端；GitHub macOS CI 自動建置未簽名 IPA，安裝時使用自己的簽署身份。
- `public/`：瀏覽器控制介面。
- `deploy/remote/`：多電話 HTTPS 入口、FRP、TURN 及授權服務。

## 建置 App

需要 Go 1.27.3、JDK 21、Gradle 8.13 和 Android SDK 34，並設定 `JAVA_HOME`、`ANDROID_HOME`。

```bash
python3 scripts/build_native.py --go go
# 建立與伺服器版本相符的 FRP 客戶端（會下載公開上游原始碼）
python3 scripts/build_frpc.py --go go
gradle :app:assembleDebug :controller:assembleDebug
```

受控端需要 ARM64 Android 及 Root。iOS 本地建置使用 Xcode 26 或更新版本，執行 `bash clients/ios/build_unsigned.sh`；亦可在 GitHub 啟動 `Verify iOS playback and trusted login` workflow，勾選 `publish_ipa` 取得 IPA。

版本及品牌設定分別位於 `release.json`、`branding/brand.json`，Icon 原稿為 `branding/icon.svg`。

## 手動部署

需要 Linux、Python 3、OpenSSL、Docker Compose 及域名／HTTPS 反代。受控程式可透過 `-enrollment-public-key` 匯出電話的 RSA 公鑰。

```bash
cd deploy/remote
python3 initialize.py --domain control.example.com --public-ip YOUR_PUBLIC_IPV4 --owner-key /path/to/your-phone.pub
# 設定自己的 HTTPS 反代到 127.0.0.1:18079，保留 Host、Origin 及 WebSocket。
docker compose up -d --build
```

將部署的 HTTPS 網址寫入受控電話的 `/data/local/tmp/remote-console/enrollment-url`，再啟動受控服務。在控制端輸入網址，即可選擇電話並要求授權。

新增另一部自己的電話：

```bash
python3 register_device.py --id secondary --name 'Android phone B' --public-key /path/to/second-phone.pub
docker compose up -d --build
```

公網需放行首部電話 TCP 7000、TURN TCP／UDP 3478 及 UDP 49160–49260；新增電話的隧道連接埠以登記程式輸出為準。設定 HTTPS 反代至 `http://127.0.0.1:18079`，保留 Host、Origin 及 WebSocket；1Panel 可直接沿用現有反代。

首次在受控電話「控制端授權」核對名稱及代碼後批准，之後即可免 PIN 登入。同一入口內的電話共用該控制端授權，授權可隨時撤銷。

## 管理選單

之後輸入 `remote-console` 開啟繁體中文選單。亦支援直接指令：

| 指令 | 功能 |
| --- | --- |
| `remote-console status` | 版本、容器狀態、網址及反代設定 |
| `remote-console start / stop / restart` | 啟動、停止或重新建立服務 |
| `remote-console logs` | 即時日誌；Ctrl+C 返回 |
| `remote-console update` | 更新至最新伺服器版本，先備份；失敗恢復舊程式 |
| `remote-console add-device` | 加入另一部電話，仍然使用同一網址 |
| `remote-console backup` | 備份自己的設定、電話身份及控制端授權 |
| `remote-console rollback` | 還原上一版程式，保留目前授權 |
| `remote-console uninstall` | 移除本專案容器，保留資料與管理指令，可重新啟用 |
| `remote-console uninstall --purge` | 額外輸入 DELETE 才刪除本工具程式及資料；備份保留 |

程式、資料及備份分別位於 `/opt/remote-console-source`、`/var/lib/remote-console-generic` 及 `/var/backups/remote-console-generic`。備份包含連線憑證，請妥善保存。

授權及上游來源見 [NOTICE.md](NOTICE.md) 與 [LICENSE](LICENSE)。
