// Native, read-only WebRTC acceptance probe for the owner's configured host.
package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/h264writer"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	origin := flag.String("origin", "https://control.example.com", "owned HTTPS origin")
	pairFile := flag.String("pairing-file", "", "private local pairing metadata")
	output := flag.String("out", "", "private acceptance artifacts")
	relay := flag.Bool("relay", false, "force authenticated TURN")
	flag.Parse()
	must(os.MkdirAll(*output, 0700))
	var pair struct {
		Pin string `json:"pin"`
	}
	data, err := os.ReadFile(*pairFile)
	must(err)
	must(json.Unmarshal(data, &pair))
	jar, err := cookiejar.New(nil)
	must(err)
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	reqBody, _ := json.Marshal(map[string]string{"pin": pair.Pin})
	req, err := http.NewRequest("POST", *origin+"/api/unlock", bytes.NewReader(reqBody))
	must(err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", *origin)
	response, err := client.Do(req)
	must(err)
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		panic(fmt.Sprintf("pairing returned HTTP %d", response.StatusCode))
	}
	getJSON := func(path string, target any) {
		r, e := client.Get(*origin + path)
		must(e)
		defer r.Body.Close()
		if r.StatusCode != 200 {
			panic(fmt.Sprintf("%s returned HTTP %d", path, r.StatusCode))
		}
		must(json.NewDecoder(r.Body).Decode(target))
	}
	var devices struct {
		Devices []struct {
			Type string `json:"device_type"`
			ID   string `json:"device_id"`
			IP   string `json:"ip"`
			Port any    `json:"port"`
		} `json:"devices"`
	}
	getJSON("/api/device/list", &devices)
	var cfg webrtc.Configuration
	getJSON("/api/ice-config", &cfg)
	if *relay {
		cfg.ICETransportPolicy = webrtc.ICETransportPolicyRelay
	}
	var session struct {
		Connected int               `json:"connected"`
		Options   map[string]string `json:"stream_options"`
	}
	getJSON("/api/screen/sessions", &session)
	options := map[string]string{"video_codec": "h264", "audio_codec": "opus", "video_bit_rate": "2000000", "max_size": "960", "max_fps": "30", "audio": "true", "control": "true", "video_codec_options": "profile=1"}
	if session.Options != nil {
		options = session.Options
	}
	pc, err := webrtc.NewPeerConnection(cfg)
	must(err)
	defer pc.Close()
	var videoPackets, audioPackets, videoFrames, videoBytes atomic.Uint64
	var orderedOpen atomic.Bool
	writer, err := h264writer.New(filepath.Join(*output, "received.h264"))
	must(err)
	defer writer.Close()
	var writeMu sync.Mutex
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			packet, _, e := track.ReadRTP()
			if e != nil {
				return
			}
			if track.Kind() == webrtc.RTPCodecTypeVideo {
				videoPackets.Add(1)
				videoBytes.Add(uint64(len(packet.Payload)))
				if packet.Marker {
					videoFrames.Add(1)
				}
				if strings.EqualFold(track.Codec().MimeType, webrtc.MimeTypeH264) {
					writeMu.Lock()
					_ = writer.WriteRTP(packet)
					writeMu.Unlock()
				}
			} else {
				audioPackets.Add(1)
			}
		}
	})
	_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	must(err)
	_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	must(err)
	ordered, err := pc.CreateDataChannel("control-ordered", nil)
	must(err)
	ordered.OnOpen(func() { orderedOpen.Store(true) })
	no := false
	_, err = pc.CreateDataChannel("control-unordered", &webrtc.DataChannelInit{Ordered: &no})
	must(err)
	offer, err := pc.CreateOffer(nil)
	must(err)
	gathered := webrtc.GatheringCompletePromise(pc)
	must(pc.SetLocalDescription(offer))
	select {
	case <-gathered:
	case <-time.After(12 * time.Second):
	}
	u, err := url.Parse(*origin)
	must(err)
	header := http.Header{"Origin": []string{*origin}}
	var cookies []string
	for _, c := range jar.Cookies(u) {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	header.Set("Cookie", strings.Join(cookies, "; "))
	wsURL := "wss://" + u.Host + "/screen/ws"
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	ws, _, err := dialer.Dial(wsURL, header)
	must(err)
	defer ws.Close()
	var selected int = -1
	for i, d := range devices.Devices {
		if d.Type == "android" {
			selected = i
			break
		}
	}
	if selected < 0 {
		panic("owned Android device not listed")
	}
	d := devices.Devices[selected]
	must(ws.WriteJSON(map[string]any{"device_type": "android", "device_id": d.ID, "device_ip": d.IP, "device_port": fmt.Sprint(d.Port), "av_sync": false, "driver_config": options, "sdp": pc.LocalDescription().SDP}))
	var capabilities map[string]any
	var meta any
	signalErrors := make(chan error, 1)
	go func() {
		for {
			var message struct {
				Status       string         `json:"status"`
				Stage        string         `json:"stage"`
				SDP          string         `json:"sdp"`
				Message      string         `json:"message"`
				Capabilities map[string]any `json:"capabilities"`
				Meta         any            `json:"media_meta"`
			}
			if e := ws.ReadJSON(&message); e != nil {
				signalErrors <- e
				return
			}
			if message.Status == "error" {
				signalErrors <- fmt.Errorf("signaling %s: %s", message.Stage, message.Message)
				return
			}
			if message.Stage == "webrtc_init" {
				if e := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: message.SDP}); e != nil {
					signalErrors <- e
					return
				}
			}
			if message.Stage == "webrtc_metainfo" {
				writeMu.Lock()
				capabilities = message.Capabilities
				meta = message.Meta
				writeMu.Unlock()
			}
		}
	}()
	started := time.Now()
	deadline := time.NewTimer(40 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-signalErrors:
			panic(err)
		case <-deadline.C:
			panic(fmt.Sprintf("WebRTC acceptance timed out: state=%s video_packets=%d control=%v", pc.ConnectionState(), videoPackets.Load(), orderedOpen.Load()))
		case <-ticker.C:
			if videoPackets.Load() > 60 && videoFrames.Load() > 5 && orderedOpen.Load() && time.Since(started) > 7*time.Second {
				goto done
			}
		}
	}
done:
	selectedPair := map[string]any{}
	if sctp := pc.SCTP(); sctp != nil {
		if pair, e := sctp.Transport().ICETransport().GetSelectedCandidatePair(); e == nil && pair != nil {
			selectedPair = map[string]any{"local_type": pair.Local.Typ.String(), "remote_type": pair.Remote.Typ.String(), "local_protocol": pair.Local.Protocol.String(), "remote_protocol": pair.Remote.Protocol.String()}
		}
	}
	writeMu.Lock()
	report := map[string]any{"origin": *origin, "force_relay": *relay, "scope": "native WebRTC receiver over the public signaling origin; not an iOS/browser UI test", "connection_state": pc.ConnectionState().String(), "ordered_control_channel_open": orderedOpen.Load(), "video_rtp_packets": videoPackets.Load(), "video_rtp_marked_frames": videoFrames.Load(), "video_payload_bytes": videoBytes.Load(), "audio_rtp_packets": audioPackets.Load(), "selected_pair": selectedPair, "capabilities": capabilities, "media_meta": meta, "duration_seconds": time.Since(started).Seconds()}
	writeMu.Unlock()
	data, err = json.MarshalIndent(report, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(*output, "acceptance.json"), data, 0600))
	fmt.Println(string(data))
}
