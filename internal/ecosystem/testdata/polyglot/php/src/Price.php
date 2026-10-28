<?php

namespace Shop;

final class Price
{
    public function __construct(private int $cents)
    {
    }

    public function discounted(int $percent): int
    {
        return intdiv($this->cents * (100 - $percent), 100);
    }
}

function format_price(int $cents): string
{
    return sprintf('%.2f', $cents / 100);
}
