resource "kafka_topic" "payments_charged" {
  name               = "payments.charged"
  partitions         = 12
  replication_factor = 3
}
