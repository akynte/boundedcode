package com.acme.ledger;

import com.acme.payments.v1.GetPaymentRequest;
import com.acme.payments.v1.PaymentServiceGrpc;
import io.grpc.ManagedChannel;
import jakarta.persistence.Entity;
import jakarta.persistence.Table;
import java.sql.Connection;

@Entity
@Table(name = "ledger_entries")
class LedgerEntry {}

public class LedgerService {
    private final PaymentServiceGrpc.PaymentServiceBlockingStub payments;
    private final Connection db;

    LedgerService(ManagedChannel channel, Connection db) {
        this.payments = PaymentServiceGrpc.newBlockingStub(channel);
        this.db = db;
    }

    void post(String paymentId) throws Exception {
        var payment = payments.withDeadlineAfter(5, java.util.concurrent.TimeUnit.SECONDS)
            .getPayment(GetPaymentRequest.newBuilder().setName("payments/" + paymentId).build());
        String sql = """
            SELECT amount_cents
              FROM payments
             WHERE id = ?
            """;
        db.prepareStatement(sql);
    }
}
