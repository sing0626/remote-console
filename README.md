# RemoteConsole · 遙控台

自託管 Android 畫面遙控：WebRTC 聲畫、觸控、文字輸入、多控制端、同一網址切換受控電話，以及首次電話批准後自動登入嘅信任控制端。

此倉庫只提供通用程式碼及範本。沒有預設伺服器網址、固定電話身份、公私鑰、個人部署資料、簽署金鑰、已簽名 App、實機錄影或舊部署歷史。每個新部署都使用自己的網址及電話公鑰，並產生獨立的隧道、TURN 和信任控制端資料。

## 原始碼

- `app/`：需要 Root 嘅 Android 受控端。
- `controller/`：原生 Android 控制端；使用者自行輸入網址。
- `clients/ios/`：原生 SwiftUI／WebRTC 控制端；需自己的 macOS／Xcode 及簽署身份。
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

受控端目前需要 ARM64 Android 及可用 Root。一般 ADB 與 Root 自投屏途徑保留不同 capture server。iOS 需要 Xcode 26 或更新版本；`clients/ios/build_unsigned.sh` 只產生未簽名 IPA，安裝前須自行簽署。iOS 音訊初始化及不同手機／網絡仍需要在自己的環境驗收，這個通用副本沒有宣稱已通過實體裝置驗收。

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

亦可在全新伺服器執行通用安裝器（需要先安裝上述工具，並準備自己的電話公鑰）：

```bash
curl -fL https://raw.githubusercontent.com/sing0626/remote-console/main/install.sh -o /tmp/remote-console-install.sh
sudo bash /tmp/remote-console-install.sh --domain control.example.com --public-ip YOUR_PUBLIC_IPV4 --owner-key /path/to/your-phone.pub
```

安裝器只接受新部署；已存在的目錄和憑證會保留並停止，不會覆蓋既有服務。

## 檢查公開內容

```bash
python3 scripts/check_public_tree.py
cd deploy/remote/gateway && go test ./...
```

公開與私有部署使用分開的檔案及 Git 歷史。CI 只使用臨時生成的測試身份；不連接現有私人伺服器，不自動公開錄影或部署輸出。授權及上游來源見 [NOTICE.md](NOTICE.md) 與 [LICENSE](LICENSE)。
