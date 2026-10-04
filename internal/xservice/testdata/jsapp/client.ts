import axios from "axios";
import { Kafka } from "kafkajs";

const PAYMENTS = process.env.PAYMENTS_URL ?? "http://payments:8080";
const api = axios.create({ baseURL: "http://inventory:9000/v1" });

export async function pay(id: string) {
  await fetch(`${PAYMENTS}/v1/payments`, { method: "POST", body: "{}" });
  await fetch(PAYMENTS + "/v1/payments/" + id);
  await axios.delete(`http://orders:3000/v2/orders/${id}`);
  await api.get(`/stock/${id}`);
  const t = process.env["LOG_LEVEL"];
  return t;
}

const kafka = new Kafka({ brokers: [] });
export async function events(producer: any, consumer: any) {
  await producer.send({ topic: "orders.created", messages: [] });
  await consumer.subscribe({ topics: ["payments.charged", "refunds"] });
}
