resource "confluent_kafka_topic" "orders" {
  topic_name = "orders.created" # literal
  partitions_count = 6
  config = {
    "retention.ms" = "604800000"
  }
}

resource "kafka_topic" "dyn" {
  name = "${var.prefix}.events"
}
