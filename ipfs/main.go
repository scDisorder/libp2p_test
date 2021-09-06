package main

import (
	"context"
	"crypto/rand"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/routing"
	kaddht "github.com/libp2p/go-libp2p-kad-dht"
	mplex "github.com/libp2p/go-libp2p-mplex"
	"github.com/libp2p/go-libp2p-pubsub"
	tls "github.com/libp2p/go-libp2p-tls"
	yamux "github.com/libp2p/go-libp2p-yamux"
	"github.com/libp2p/go-libp2p/p2p/discovery"
	"github.com/libp2p/go-tcp-transport"
	"github.com/libp2p/go-ws-transport"
	"github.com/multiformats/go-multiaddr"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type mdnsNotifee struct {
	h   host.Host
	ctx context.Context
}

func (m *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	m.h.Connect(m.ctx, pi)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	transports := libp2p.ChainOptions(
		libp2p.Transport(tcp.NewTCPTransport), // this refers to multiaddrs usage
		libp2p.Transport(websocket.New),       // same, as one above
	)

	muxers := libp2p.ChainOptions(
		libp2p.Muxer("/yamux/1.0.0", yamux.DefaultTransport),
		libp2p.Muxer("/mplex/6.7.0", mplex.DefaultTransport),
	)

	security := libp2p.Security(tls.ID, tls.New)

	listenAddrs := libp2p.ListenAddrStrings(
		"/ip4/0.0.0.0/tcp/0",
		"/ip4/0.0.0.0/tcp/0/ws",
	)

	var dht *kaddht.IpfsDHT
	newDHT := func(h host.Host) (routing.PeerRouting, error) {
		var err error
		if dht, err = kaddht.New(ctx, h); err != nil {
			log.Fatalf("Unable to create dht: %s", err)
		}
		return dht, nil
	}
	router := libp2p.Routing(newDHT)

	r := rand.Reader
	prvKey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	if err != nil {
		log.Fatal(err)
	}

	h, err := libp2p.New(
		ctx,
		transports,
		listenAddrs,
		muxers,
		security,
		router,
		libp2p.Identity(prvKey),
	)
	if err != nil {
		log.Fatal(err)
	}

	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		log.Fatal(err)
	}

	topic, err := ps.Join(pubsubTopic)
	if err != nil {
		log.Fatal(err)
	}

	defer topic.Close()
	sub, err := topic.Subscribe()
	if err != nil {
		log.Fatal(err)
	}
	go pubsubHandler(ctx, sub)

	for _, addr := range h.Addrs() {
		log.Println("Listening on", addr)
	}

	targetAddr, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/9420/p2p/QmWjz6xb8v9K4KnYEwP5Yk75k5mMBCehzWFLCvvQpYxF3d")
	if err != nil {
		log.Fatalf("Failed to connect target: %s", err)
	}

	targetInfo, err := peer.AddrInfoFromP2pAddr(targetAddr)
	if err != nil {
		log.Fatalf("Unable to get target info: %s", err)
	}

	log.Println("Connected to", targetInfo.ID)

	mdns, err := discovery.NewMdnsService(ctx, h, 5*time.Second, "")
	if err != nil {
		log.Fatalf("Unable to create new mdns service: %s", err)
	}
	mdns.RegisterNotifee(&mdnsNotifee{h: h, ctx: ctx})

	if err := dht.Bootstrap(ctx); err != nil {
		log.Fatalf("Failed to bootstrap dht: %s", err)
	}

	donec := make(chan struct{}, 1)
	go chatInputLoop(ctx, h, topic, donec)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT)

	select {
	case <-stop:
		h.Close()
		os.Exit(0)
	case <-donec:
		h.Close()
	}
}
