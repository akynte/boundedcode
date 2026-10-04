package main

import (
	"context"

	"github.com/IBM/sarama"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	kafkago "github.com/segmentio/kafka-go"
	"github.com/twmb/franz-go/pkg/kgo"

	"example.com/gokafka/topics"
)

func produceSarama(p sarama.SyncProducer) {
	p.SendMessage(&sarama.ProducerMessage{Topic: topics.OrderCreated})
}

func consumeSarama(ctx context.Context, g sarama.ConsumerGroup, h sarama.ConsumerGroupHandler) {
	g.Consume(ctx, []string{"payments.charged", "refunds"}, h)
}

func kafkaGo() {
	w := &kafkago.Writer{Topic: "audit.log"}
	_ = w
	r := kafkago.NewReader(kafkago.ReaderConfig{Topic: topics.OrderCreated, GroupID: "g"})
	_ = r
}

func franz(cl *kgo.Client) {
	_ = kgo.ConsumeTopics("shipments", "returns")
	cl.Produce(context.Background(), &kgo.Record{Topic: "shipments"}, nil)
}

func confluent(c *kafka.Consumer) {
	c.SubscribeTopics([]string{"invoices"}, nil)
	topic := "invoices.dlq"
	_ = kafka.Message{TopicPartition: kafka.TopicPartition{Topic: &topic}}
}
