package redis

import (
	"fmt"
	"log"
)

const (
	ChannelProxy    = "proxy"
	ChannelUnlocker = "unlocker"
	ChannelApi 		= "api"
	ChannelPayout 	= "payout"
)

const (
	OpcodeLoadID 	= "inbound-id"
	OpcodeLoadIP 	= "inbound-ip"
	OpcodeWhiteList = "white-list"
	OpcodeMinerSub 	= "miner-sub"
)

type PubSub interface {
	RedisMessage(string)
}

func (r *RedisClient) InitPubSub(name string, proc PubSub) {
	psc, err := r.client.Subscribe(name)
	if err != nil {
		log.Fatalf("redis pub/sub subscribe failed: %s",name)
	}

	go func() {
		for {
			v, err := psc.ReceiveMessage()
			if err != nil {
				return
			}
			proc.RedisMessage(v.Payload)
			fmt.Printf("(%s)msg : %s\n", v.Channel, v.Payload)
		}
	}()
}

func (r *RedisClient) Publish(channel string, opcode string, data string, reply string) (int64,error){
	msg := opcode + ":" + channel + ":" + data
	res, err := r.client.Publish(channel, msg).Result()
	if err != nil {
		return 0, err
	}
	return res, nil
}