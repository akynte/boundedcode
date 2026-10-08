"""Checkout calls payments over gRPC.

A docstring mentioning add_FakeServicer_to_server( is not code.
"""
import grpc
from acme.payments.v1 import payment_pb2, payment_pb2_grpc


class Checkout:
    def __init__(self, channel, db):
        self.stub = payment_pb2_grpc.PaymentServiceStub(channel)
        self.db = db

    def pay(self, account_id, cents):
        # stub.DeletePayment(...) in a comment is not a call
        return self.stub.CreatePayment(payment_pb2.CreatePaymentRequest(account_id=account_id, amount_cents=cents))

    def recent(self, account_id):
        cur = self.db.cursor()
        cur.execute(
            "SELECT p.id, p.amount_cents "
            "FROM payments p "
            "WHERE p.account_id = %s",
            (account_id,),
        )
        return cur.fetchall()
