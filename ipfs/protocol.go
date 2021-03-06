package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-pubsub"
	"log"
	"os"
	"strings"
	"time"
)

func sendMessage(ctx context.Context, topic *pubsub.Topic, msg string) {
	msgId := make([]byte, 10)
	_, err := rand.Read(msgId)
	defer func() {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
	if err != nil {
		return
	}
	now := time.Now().Unix()
	req := &Request{
		Type: Request_SEND_MESSAGE,
		SendMessage: &SendMessage{
			Id:      msgId,
			Data:    []byte(msg),
			Created: now,
		},
	}
	msgBytes, err := json.Marshal(req)
	if err != nil {
		return
	}
	err = topic.Publish(ctx, msgBytes)
}

func updatePeer(ctx context.Context, topic *pubsub.Topic, id peer.ID, handle string) {
	oldHandle, ok := handles[id.String()]
	if !ok {
		oldHandle = id.ShortString()
	}
	handles[id.String()] = handle

	req := &Request{
		Type: Request_UPDATE_PEER,
		UpdatePeer: &UpdatePeer{
			UserHandle: []byte(handle),
		},
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		log.Printf("Failed to marshal payload: %s", err)
		return
	}

	if err := topic.Publish(ctx, reqBytes); err != nil {
		log.Printf("Failed to publish: %s", err)
		return
	}

	fmt.Printf("%s -> %s\n", oldHandle, handle)
}

func chatInputLoop(ctx context.Context, h host.Host, topic *pubsub.Topic, donec chan struct{}) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		msg := scanner.Text()
		if strings.HasPrefix(msg, "/name ") {
			newHandle := strings.TrimPrefix(msg, "/name ")
			newHandle = strings.TrimSpace(newHandle)
			updatePeer(ctx, topic, h.ID(), newHandle)
		} else {
			sendMessage(ctx, topic, msg)
		}
	}
	donec <- struct{}{}
}
