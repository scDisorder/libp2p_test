package main

import (
	"context"
	"fmt"
	"github.com/libp2p/go-libp2p"
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
		dht, err = kaddht.New(ctx, h)
		return dht, err
	}
	routing := libp2p.Routing(newDHT)

	host, err := libp2p.New(
		ctx,
		transports,
		listenAddrs,
		muxers,
		security,
		routing,
	)
	if err != nil {
		panic(err)
	}

	ps, err := pubsub.NewGossipSub(ctx, host)
	if err != nil {
		panic(err)
	}
	topic, err := ps.Join(pubsubTopic)
	if err != nil {
		panic(err)
	}
	defer topic.Close()
	sub, err := topic.Subscribe()
	if err != nil {
		panic(err)
	}
	go pubsubHandler(ctx, sub)

	for _, addr := range host.Addrs() {
		fmt.Println("Listening on", addr)
	}

	targetAddr, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/63785/p2p/QmWjz6xb8v9K4KnYEwP5Yk75k5mMBCehzWFLCvvQpYxF3d")
	if err != nil {
		log.Fatalf("Failed to connect target: %s", err)
	}

	targetInfo, err := peer.AddrInfoFromP2pAddr(targetAddr)
	if err != nil {
		//log.Printf("Unable to get target info: %s", err)
		//err = nil
		log.Fatalf("Unable to get target info: %s", err)
	}

	fmt.Println("Connected to", targetInfo.ID)

	mdns, err := discovery.NewMdnsService(ctx, host, 5*time.Second, "")
	if err != nil {
		log.Fatalf("Unable to create new mdns service")
	}
	mdns.RegisterNotifee(&mdnsNotifee{h: host, ctx: ctx})

	err = dht.Bootstrap(ctx)
	if err != nil {
		panic(err)
	}

	donec := make(chan struct{}, 1)
	go chatInputLoop(ctx, host, topic, donec)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT)

	select {
	case <-stop:
		host.Close()
		os.Exit(0)
	case <-donec:
		host.Close()
	}
}
