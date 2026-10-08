use tonic::transport::Server;
use payments::refund_service_server::RefundServiceServer;

// A lifetime 'a and a raw string must not confuse the lexer.
fn label<'a>(s: &'a str) -> &'a str { s }

const QUERY: &str = r#"UPDATE payments SET status = 'refunded' WHERE id = $1"#;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    Server::builder().add_service(RefundServiceServer::new(Refunds::default())).serve(addr).await?;
    Ok(())
}
