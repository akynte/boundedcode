import express from "express";
import { Controller, Get, Post } from "@nestjs/common";

const app = express();
const API = "/api";
app.post(`${API}/carts/:cartId/checkout`, async (req, res) => {
  res.send({ ok: true });
});
app.get("/healthz", (_req, res) => res.send("ok")); // route
const re = /\/not-a-route\//g; // regex literal must not confuse the lexer

@Controller("orders")
export class OrdersController {
  @Post()
  create() {}
  @Get(":id")
  find() {}
}
