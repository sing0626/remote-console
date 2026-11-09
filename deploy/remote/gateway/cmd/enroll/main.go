package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"remoteconsole.local/connect/client"
	"remoteconsole.local/connect/envelope"
)

func main() {
	identity := flag.String("identity", "enrollment.key", "private device identity file")
	public := flag.Bool("public-key", false, "print only public device identity")
	origin := flag.String("origin", "", "HTTPS server origin")
	out := flag.String("out", "remote", "private profile output directory")
	connectAddress := flag.String("connect-address", "", "optional loopback routing for isolated HTTPS tests; certificate verification stays enabled")
	serverIP := flag.String("server-ip", "", "public server IPv4 for Android root networking; TLS hostname validation stays enabled")
	flag.Parse()
	key, err := client.Identity(*identity)
	if err != nil {
		log.Fatal("cannot load private identity")
	}
	if *public {
		os.Stdout.Write(envelope.PublicPEM(&key.PublicKey))
		return
	}
	var transport http.RoundTripper
	if *serverIP != "" {
		transport, err = client.TransportForServer(*origin, *serverIP)
		if err != nil {
			log.Fatal(err)
		}
	}
	if *connectAddress != "" {
		host, _, splitErr := net.SplitHostPort(*connectAddress)
		if splitErr != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			log.Fatal("test routing must be a loopback address")
		}
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = nil
		t.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, *connectAddress)
		}
		transport = t
	}
	bundle, err := client.FetchWithTransport(context.Background(), *origin, key, transport)
	if err != nil {
		log.Fatal(err)
	}
	if err = client.Save(*out, *origin, bundle); err != nil {
		log.Fatal("cannot save imported profile")
	}
	fmt.Println("Encrypted profile imported; private credentials were not printed.")
}
