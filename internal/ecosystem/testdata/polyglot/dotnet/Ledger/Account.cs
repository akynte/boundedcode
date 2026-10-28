namespace Shop.Ledger;

public sealed class Account
{
    public long Balance { get; private set; }

    public void Post(long cents)
    {
        Balance += cents;
    }
}

public record Entry(string AccountId, long Cents);
