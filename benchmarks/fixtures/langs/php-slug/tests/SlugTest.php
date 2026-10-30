<?php

declare(strict_types=1);

function test_simple_title(): void
{
    assert_same('hello-world', slugify('Hello World'));
}

function test_accents(): void
{
    assert_same('cafe-creme', slugify('Café Crème'));
}
