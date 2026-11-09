# 半雲電話 · HalfCloud

自託管 Android 畫面遙控：WebRTC 聲畫、觸控、文字輸入、多控制端、同一網址切換受控電話，以及首次電話批准後自動登入嘅信任控制端。

此倉庫只提供通用程式碼及範本。沒有預設伺服器網址、固定電話身份、公私鑰、個人部署資料、簽署金鑰、已簽名 App、實機錄影或舊部署歷史。每個新部署都使用自己的網址及電話公鑰，並產生獨立的隧道、TURN 和信任控制端資料。


## 統一版本 0.4.0

四個元件使用同一份 `release.json`（0.4.0 / 1040）及 `branding/icon.svg`：iOS 控制端、Android 控制端、Android 受控端、VPS 服務。原生控制端有統一品牌、裝置入口、版本標示及獨立串流數據列；數據不再覆蓋電話畫面。iOS 保留互動 Liquid Glass capsule，並非 WebView。

通用 APK、未簽名 IPA 及精簡伺服器套件見 [0.4.0 發佈頁](https://github.com/sing0626/remote-console/releases/tag/v0.4.0)。公開 App 不預設伺服器或電話身份。既有不同簽署／套件的私人安裝必須使用其原簽署的相容升級 APK，不能藉清除資料或重裝來保留既有授權。

舊版伺服器統一升級命令（root）：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/sing0626/remote-console/v0.4.0/install.sh) upgrade --release v0.4.0
remote-console
```

命令自動選擇已管理部署更新或 v0.1.14 原位遷移；沿用現有網址、電話 ID、配對及控制端授權。先備份，再切換服務。精簡包約 65 KB，下載會顯示進度、對停滯重試並驗證 GitHub 發佈的 SHA-256；不再為 VPS 下載整個手機 SDK／素材來源包。管理指令 `remote-console` 保留相容。

## 原始碼

- `app/`：需要 Root 嘅 Android 受控端。
- `controller/`：原生 Android 控制端；使用者自行輸入網址。
- `clients/ios/`：原生 SwiftUI／WebRTC 控制端；GitHub macOS CI 自動建置未簽名 IPA，安裝時使用自己的簽署身份。
- `public/`：瀏覽器控制介面。
- `deploy/remote/`：多電話 HTTPS 入口、FRP、TURN 及授權服務。

## 建置 Android

需要 Go 1.27.3、JDK 21、Gradle 8.13 和 Android SDK 34，並設定 `JAVA_HOME`、`ANDROID_HOME`。

```bash
python3 scripts/build_native.py --go go
# 建立與伺服器版本相符的 FRP 客戶端（會下載公開上游原始碼）
python3 scripts/build_frpc.py --go go
# 不要使用其他部署的私有設定或簽署金鑰。
gradle :app:assembleDebug :controller:assembleDebug
```

受控端目前需要 ARM64 Android 及可用 Root。一般 ADB 與 Root 自投屏途徑保留不同 capture server。iOS 本地建置需要 Xcode 26 或更新版本；亦可直接下載 [iOS 0.4.0 未簽名 IPA](https://github.com/sing0626/remote-console/releases/tag/v0.4.0)，或手動啟動 `Verify iOS playback and trusted login` workflow，勾選 `publish_ipa`。Windows 使用者毋須本地 Mac，IPA 安裝前仍須自行簽署。

iOS 0.4.0 使用播放專用 PCM 音訊裝置，不要求麥克風權限；首次電話批准後將憑證存於 Keychain，之後免 PIN 重連。已有 [8/8 iOS 26.2 模擬器 UI／真 WebRTC fixture 測試](https://github.com/sing0626/remote-console/actions/runs/42584224712)，包括有聲 PCM、兩電話切換、重新連線及撤銷後重新批准。實體 iPhone、音訊路由／中斷及不同公網環境仍需在自己的環境驗收。

## 建立自己的伺服器

需要 Linux、Python 3、OpenSSL、Docker Compose，以及自己的域名／HTTPS 反代。從自己的受控電話匯出 RSA 公鑰；私鑰留在電話。受控程式的 `-enrollment-public-key` 選項只輸出公鑰。

```bash
cd deploy/remote
python3 initialize.py --domain control.example.com --public-ip YOUR_PUBLIC_IPV4 --owner-key /path/to/your-phone.pub
# 設定自己的 HTTPS 反代到 127.0.0.1:18079，保留 Host、Origin 及 WebSocket。
docker compose up -d --build
```

受控電話的部署網址位於其 Root 工作目錄 `/data/local/tmp/remote-console/enrollment-url`；將自己的 HTTPS 網址寫入後重新啟動受控服務。設定會以該電話公鑰加密，電話私鑰不離開電話。不要把 `private/` 或電話實際公鑰提交到倉庫。

新增另一部自己的電話：

```bash
python3 register_device.py --id secondary --name 'Android phone B' --public-key /path/to/second-phone.pub
docker compose up -d --build
```

公網需放行首部電話 TCP 7000、TURN TCP／UDP 3478 及 UDP 49160–49260；新增電話的隧道 port 以登記程式輸出為準。反代及 18079／18081 等後端埠保留本機使用。不附帶 Caddy，使用自己的 HTTPS 反代即可。

首次在控制端要求授權，於受控電話「控制端授權」核對名稱及代碼後批准。之後同一入口內的電話共用該控制端授權，並可撤銷其連線。代碼用作核對，不需輸入電話 PIN。

### 一行安裝與管理選單

全新 Ubuntu / Debian 主機以 root 執行以下一行。安裝器自動補齊 Python、OpenSSL、Docker Compose，然後詢問自己的域名、公網 IPv4 及電話公鑰（檔案路徑或直接貼 PEM）：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/sing0626/remote-console/main/install.sh)
```

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

程式位於 `/opt/remote-console-source`，資料位於 `/var/lib/remote-console-generic`，備份位於 `/var/backups/remote-console-generic`。更新下載固定 commit 的原始碼，先建置新版再切換；不重新產生現有配對密鑰、TURN 設定或控制端憑證。備份包含自己的憑證，權限 0600，請自行保存，勿上傳公開倉庫。解除安裝只處理本工具的 Docker 專案，不移除 Docker、不做全域 prune。

通用安裝器只管理自己標記的部署；遇到其他既有安裝、未知目錄或相同 port 會停止並保留現有內容。需要 HTTPS 反代與防火牆設定，安裝後會列出要求；不會安裝 Caddy 或佔用 80/443。手動部署仍可沿用上面的 `initialize.py`／Compose 流程，但不會自動納入管理選單。

### 舊版 v0.1.14 升級至管理選單

如果目前使用舊安裝器的 `/opt/remote-console`、`remote-console` Docker 專案及 v0.1.14，請使用遷移命令，毋須重新輸入域名、公鑰或電話 PIN：

```bash
# 以 root 執行；先備份，切換時會短暫重啟服務。
bash <(curl -fsSL https://raw.githubusercontent.com/sing0626/remote-console/v0.2.1/install.sh) migrate --release v0.2.1
remote-console status
remote-console
```

遷移前核對安裝版本、容器歸屬、電話登記資料及隧道設定；先建置新版，再停止 gateway 取得完整舊部署備份。沿用 `/opt/remote-console/private` 及原本 Docker 專案，不改電話 ID、名稱、隧道 token、TURN secret、證書、電話設定內的路徑或控制端憑證。原始碼另外放在 `/opt/remote-console-source`，原有程式及 Compose 保留在 `/opt/remote-console`。備份放在 `/var/backups/remote-console-generic/legacy-v0.1.14-*.tar.gz`，權限 0600；請自行妥善保存。

啟動或身份核對失敗會嘗試恢復原有程式服務，不會用舊備份覆蓋目前授權紀錄，避免已撤銷的控制端復活。升級完成後可用 `remote-console update` 更新；新增電話仍沿用同一網址。現有電話會自行重連，不需要重新配對、改 DNS、反代或防火牆。只接受已知 v0.1.14 結構；遇到其他版本、未知服務或額外資料掛載會停止，保留現有部署供核對。

## 檢查公開內容

```bash
python3 scripts/check_public_tree.py
cd deploy/remote/gateway && go test ./...
```

公開與私有部署使用分開的檔案及 Git 歷史。CI 只使用臨時生成的測試身份；不連接現有私人伺服器，不自動公開錄影或部署輸出。授權及上游來源見 [NOTICE.md](NOTICE.md) 與 [LICENSE](LICENSE)。
