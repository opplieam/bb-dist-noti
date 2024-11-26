package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	api "github.com/opplieam/bb-dist-noti/protogen/category_v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func main() {
	nc, err := nats.Connect("nats://localhost:4222")
	if err != nil {
		log.Fatal("Error connecting to nats server: ", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        "update",
		Description: "Message for update",
		Subjects: []string{
			"update.>",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	i := 0
	for {
		i++
		time.Sleep(1 * time.Second)
		msg := &api.CategoryMessage{
			UserId:       1,
			CategoryFrom: fmt.Sprintf("category-%d", i),
			CategoryTo:   fmt.Sprintf("match-category-%d", i),
			CreatedAt:    timestamppb.New(time.Now()),
		}

		b, _ := proto.Marshal(msg)
		_, err = js.Publish(ctx, fmt.Sprintf("update.category.%d", i), b)
		if err != nil {
			log.Println("Error publishing message: ", err)
			continue
		}
		log.Printf("Published Category: [%d]", i)
	}
}
