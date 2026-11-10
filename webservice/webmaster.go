package webservice

import (
	"crypto/rand"
	"io/fs"
	"log"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/pion/webrtc/v4"
	"webscreen/devicetools"
	sagent "webscreen/streamAgent"
	"webscreen/utils"
	"webscreen/webservice/android"
)

type WebMasterConfig struct {
	EnableDeviceTools     bool
	SharedDirectory       string
	ToolStateDirectory    string
	Version               string
	Loopback              bool
	EnableAndroidDiscover bool
}

type WebMaster struct {
	tools           *devicetools.Manager
	gatewayMu       sync.RWMutex
	gatewayIdentity *gatewayIdentity
	// WSConns []*websocket.Conn

	WebRTCManager *WebRTCManager
	// ScreenSessions map[string]ScreenSession

	pin                  string
	UnlockAttemptRecords map[string]UnlockAttemptRecord
	unlockMu             sync.Mutex
	backlightMu          sync.Mutex
	backlightOff         bool
	jwtSecret            []byte

	config              WebMasterConfig
	router              *gin.Engine
	devicesConnected    map[string]Device
	devicesDiscovered   map[string]Device
	devicesDiscoveredMu sync.RWMutex
	pauseDiscovery      bool
	staticFS            fs.FS
}

func New(config WebMasterConfig, staticFS fs.FS) *WebMaster {
	wm := &WebMaster{
		// ScreenSessions:       make(map[string]ScreenSession),
		config:               config,
		devicesDiscovered:    make(map[string]Device),
		staticFS:             staticFS,
		UnlockAttemptRecords: make(map[string]UnlockAttemptRecord),
		WebRTCManager:        NewWebRTCManager(),
	}
	wm.jwtSecret = make([]byte, 32)
	wm.WebRTCManager.onLastSubscriber = wm.restoreLocalBacklight
	wm.createTools()
	if _, err := rand.Read(wm.jwtSecret); err != nil {
		panic("cannot initialize authentication key")
	}
	return wm
}

func Default(staticFS fs.FS) *WebMaster {
	wm := New(WebMasterConfig{
		EnableAndroidDiscover: true,
	}, staticFS)
	return wm
}

func (wm *WebMaster) setRouter() {
	// gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	_ = r.SetTrustedProxies(nil)
	r.Use(func(c *gin.Context) {
		if !sameOrigin(c.Request) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "origin rejected"})
			return
		}
		c.Next()
	})

	// 前端资源是 //go:embed 进二进制的：升级后浏览器若沿用缓存的旧 HTML/JS，
	// 会出现"按钮在但点了没反应"（旧页面没有新脚本标签）这类极难排查的现象。
	// 因此统一禁止缓存，保证升级后刷新即生效。
	r.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Next()
	})

	subFS, _ := fs.Sub(wm.staticFS, "static")
	r.StaticFS("/static", http.FS(subFS))

	r.GET("/unlock", func(ctx *gin.Context) {
		ctx.FileFromFS("marble-unlock.html", http.FS(wm.staticFS))
	})
	r.POST("/api/unlock", wm.handleUnlock)

	r.GET("/", func(ctx *gin.Context) {
		ctx.Redirect(302, "/control")
	})
	r.Use(wm.HybridAuthMiddleware())
	r.Any("/api/tools/*path", wm.deviceTools)
	r.GET("/control", func(c *gin.Context) { c.FileFromFS("control.html", http.FS(wm.staticFS)) })
	screen := r.Group("/screen")
	{
		screen.GET("/:id", func(ctx *gin.Context) {
			ctx.FileFromFS("screen.html", http.FS(wm.staticFS))
		})
		screen.GET("/ws", wm.handleScreenWS)
	}

	r.GET("/console", func(c *gin.Context) {
		c.FileFromFS("console.html", http.FS(wm.staticFS))
	})
	api := r.Group("/api")
	{
		api.GET("/device/list", wm.handleListDevices)
		api.POST("/device/connect", wm.handleConnectDevice)
		api.POST("/device/pair", wm.handlePairDevice)
		api.GET("/device/configDescription", wm.handleDeviceConfigDescription)
		// api.GET("/generalConfigDescription", wm.handleGeneralConfigDescription)

		// api.POST("/device/discovery", wm.handleListDevicesDiscoveried)
		// api.POST("/setPIN", wm.handleSetPIN)

		// 统一设备管理（USB / Wi-Fi）：设备列表、事件日志、命名与连接控制
		api.GET("/adb/devices", wm.handleADBDevices)
		api.GET("/adb/events", wm.handleADBEvents)
		api.POST("/adb/usb/rescan", wm.handleADBRescanUSB)
		api.POST("/adb/device/name", wm.handleADBSetName)
		api.POST("/adb/device/wifi", wm.handleADBAddWiFi)
		api.POST("/adb/device/remove", wm.handleADBRemove)
		api.POST("/adb/device/disconnect", wm.handleADBDisconnect)
		api.POST("/adb/device/reconnect", wm.handleADBReconnect)

		// 受控端背光（隐私保护：屏幕黑，但不锁屏，串流照常）
		api.POST("/screen/backlight", wm.handleBacklight)
		api.GET("/gateway-trust/status", wm.handleGatewayTrustStatus)
		api.GET("/gateway-trust/list", wm.handleGatewayTrustList)
		api.POST("/gateway-trust/approve", wm.handleGatewayTrustApprove)
		api.POST("/gateway-trust/revoke", wm.handleGatewayTrustRevoke)
		api.GET("/ice-config", func(c *gin.Context) {
			if !wm.WebRTCManager.hasRelayRegion(c.Query("region")) {
				c.JSON(400, gin.H{"error": "unknown_relay_region"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"iceServers": wm.WebRTCManager.iceServersForRegion(c.Query("region")), "relay_regions": wm.WebRTCManager.relayRegions()})
		})
		api.GET("/screen/sessions", func(c *gin.Context) {
			count := wm.WebRTCManager.connectedCount()
			options := wm.WebRTCManager.activeStreamOptions()
			wm.backlightMu.Lock()
			off := wm.backlightOff
			wm.backlightMu.Unlock()
			c.JSON(http.StatusOK, gin.H{"connected": count, "limit": MAX_CLIENTS_PER_DEVICE, "backlight_off": off, "stream_options": options})
		})
	}

	wm.router = r
}

func (wm *WebMaster) SetPIN(pin string) {
	// Set the PIN for web access
	// This is a placeholder implementation
	log.Print("Pairing authentication configured")
	wm.pin = pin
}

func (wm *WebMaster) Serve(host, port string) {
	// if wm.config.EnableAndroidDiscover {
	// 	go wm.AndroidDevicesDiscovery()
	// }
	// 统一设备管理器：USB 热插拔、RSA 授权状态与无线重连都由它跟踪。
	// 本机 root 模式下没有 adb，不需要它。
	if !utils.IsLocalRootMode() {
		android.GetManager().Start()
	}
	wm.setRouter()
	if wm.config.Loopback && host != "127.0.0.1" && host != "0.0.0.0" {
		go func() {
			if err := wm.router.Run("127.0.0.1:" + port); err != nil {
				log.Printf("Loopback tunnel listener failed: %v", err)
			}
		}()
	}
	err := wm.router.Run(host + ":" + port)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func (wm *WebMaster) Close() {
	if wm.tools != nil {
		wm.tools.Close()
	}
	wm.WebRTCManager.RLock()
	var agents []*sagent.Agent
	var peers []*webrtc.PeerConnection
	for _, b := range wm.WebRTCManager.broadcasters {
		if b.Agent != nil {
			agents = append(agents, b.Agent)
		}
		b.Lock.RLock()
		for _, s := range b.Subscribers {
			peers = append(peers, s.PeerConnection)
		}
		b.Lock.RUnlock()
	}
	wm.WebRTCManager.RUnlock()
	for _, p := range peers {
		_ = p.Close()
	}
	for _, a := range agents {
		a.Close()
	}
	// for k, v := range maps.All(wm.ScreenSessions) {
	// 	log.Printf("closing session %v", k)
	// 	v.Close()
	// }
}
