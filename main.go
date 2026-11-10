package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"remoteconsole.local/connect/releaseinfo"
	"strings"
	"syscall"
	"webscreen/utils"
	"webscreen/webservice"
)

//go:embed public
var publicFS embed.FS

func main() {
	showVersion := flag.Bool("version", false, "print suite version and exit")
	host := flag.String("host", "0.0.0.0", "host to bind the server to")
	port := flag.String("port", "8081", "server port")
	pin := flag.String("pin", "", "initial PIN for web access")
	pinFile := flag.String("pin-file", "", "private file containing the pairing PIN")
	iceFile := flag.String("ice-config", "", "private optional STUN/TURN configuration")
	loopback := flag.Bool("loopback", false, "also listen on loopback for a separate outbound signaling tunnel")
	enrollmentPublicKey := flag.Bool("enrollment-public-key", false, "print this device's enrollment public key and exit")
	// local-root runs webscreen on the Android device itself (needs root):
	// no adb is used, the scrcpy server is started locally with app_process.
	localRoot := flag.Bool("local-root", false, "run on the device itself (root, no adb): the local Android device is streamed")
	flag.Parse()
	if *showVersion {
		fmt.Println(releaseinfo.Version)
		return
	}
	if *enrollmentPublicKey {
		if err := printEnrollmentIdentity(); err != nil {
			log.Fatal("Cannot prepare device enrollment identity")
		}
		return
	}
	if *pinFile != "" {
		data, err := os.ReadFile(*pinFile)
		if err != nil {
			log.Fatal("Cannot read pairing PIN file")
		}
		*pin = strings.TrimSpace(string(data))
	}
	if *pin == "" {
		log.Fatal("A six-digit pairing PIN is required")
	}
	// pin should be 6 digits and only digits
	if *pin != "" {
		if len(*pin) != 6 {
			log.Fatal("PIN must be exactly 6 digits")
		}
		for _, ch := range *pin {
			if ch < '0' || ch > '9' {
				log.Fatal("PIN must contain only digits")
			}
		}
	}

	if *pin == "" {
		log.Println("Warning: Since v1.3.6, the default PIN is empty, which means no PIN is required to access the web interface.")
	}

	utils.SetLocalRootMode(*localRoot)
	if utils.IsLocalRootMode() {
		log.Println("On-device mode enabled: adb is not used, the local Android device will be streamed")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pub, _ := fs.Sub(publicFS, "public")
	iceConfig, err := webservice.ReadICEConfiguration(*iceFile)
	if err != nil {
		log.Fatal("Cannot read private ICE configuration")
	}
	webMaster := webservice.New(webservice.WebMasterConfig{EnableAndroidDiscover: true, Loopback: *loopback, EnableDeviceTools: *localRoot, ToolStateDirectory: filepath.Join(enrollmentWork, "features"), Version: releaseinfo.Version}, pub)
	webMaster.SetICEConfiguration(iceConfig)
	webMaster.SetPIN(*pin)

	go webMaster.Serve(*host, *port)
	enrollmentDone := make(chan struct{})
	if *localRoot {
		go func() { defer close(enrollmentDone); runEnrollment(ctx, webMaster) }()
	} else {
		close(enrollmentDone)
	}

	<-ctx.Done()
	log.Println("Gracefully closing")
	webMaster.Close()
	<-enrollmentDone

}
