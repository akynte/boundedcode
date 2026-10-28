namespace Shop.Ledger.Tests;

public class AccountTests
{
    public void Posts()
    {
        var a = new Account();
        a.Post(5);
    }
}
